package ioboard

import (
	"github.com/apex/log"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/zserge/hid"
	"time"
	"usbioboard_exporter/utils"
)

const cmdGetRegBit = 0x9a
const cmdSetRegBit = 0x9b

const regAnsel = 0x5b
const regTris = 0x92
const regPort = 0x80
const regIntCon2 = 0xf1
const regWpuB = 0x85

var configError = errors.New("config error")

type PinConfig struct {
	Name   string
	Help   string
	Port   string
	Pin    byte
	PullUp bool `yaml:"pull_up"`
	Revert bool
	Labels prometheus.Labels
}

type Config struct {
	Bus         int
	Device      int
	Prefix      string
	ReadDelayMs int `yaml:"read_delay_ms"`
	Pins        []*PinConfig
}

func (c *Config) UnmarshalYAML(unmarshal func(interface{}) error) error {
	c.Prefix = "ioboard"
	c.ReadDelayMs = 1000
	type plain Config
	return unmarshal((*plain)(c))
}

type pinData struct {
	log    *log.Entry
	port   byte
	pin    byte
	revert bool
	value  prometheus.Gauge
}

type Exporter struct {
	dev hid.Device
	cfg *Config

	stoppedChan chan struct{}
	stopChan    chan struct{}

	log *log.Entry
}

func New(config *Config) *Exporter {
	return &Exporter{
		cfg:         config,
		stoppedChan: make(chan struct{}),
		stopChan:    make(chan struct{}),
	}
}

func findIoBoard(busId, devId int) hid.Device {
	var device hid.Device

	hid.UsbWalk(func(d hid.Device) {
		info := d.Info()

		if info.Vendor == 0x04d8 && info.Product == 0x003f &&
			(busId == 0 || busId == info.Bus) && (devId == 0 || devId == info.Device) {
			device = d
		}
	})

	return device
}

func (e *Exporter) setRegBit(reg, regBit, bVal byte) (byte, error) {
	msg := []byte{cmdSetRegBit, 0, 0, 0, 0, 0, 0, 0, 0, 0, reg, regBit, bVal, 0}

	_, err := e.dev.Write(msg, time.Millisecond*100)
	if err != nil {
		return 0, err
	}

	msg, err = e.dev.Read(64, time.Millisecond*100)
	if err != nil {
		return 0, err
	}

	if len(msg) < 2 {
		return 0, errors.New("invalid data")
	}

	return msg[1], nil
}

func (e *Exporter) getRegBit(reg, regBit byte) (byte, error) {
	msg := []byte{cmdGetRegBit, 0, 0, 0, 0, 0, 0, 0, 0, 0, reg, regBit, 0, 0}

	_, err := e.dev.Write(msg, time.Millisecond*100)
	if err != nil {
		return 0, err
	}

	msg, err = e.dev.Read(64, time.Millisecond*100)
	if err != nil {
		return 0, err
	}

	if len(msg) < 2 {
		return 0, errors.New("invalid data")
	}

	return msg[1], nil
}

func portIndex(name string) byte {
	if len(name) == 1 {
		if name[0] >= 'a' && name[0] <= 'e' {
			return name[0] - 'a'
		} else if name[0] >= 'A' && name[0] <= 'E' {
			return name[0] - 'A'
		}
	}

	return 255
}

func isPinAllowed(port, pin byte) bool {
	if pin < 0 || pin > 7 {
		return false
	}

	if port == 2 && (pin >= 3 && pin <= 5) {
		return false
	}

	if port == 4 && pin > 3 {
		return false
	}

	return true
}

func isPinWired(port, pin byte) bool {
	if !isPinAllowed(port, pin) {
		return false
	}

	if port == 2 && pin == 2 {
		return false
	}

	if port == 3 && (pin >= 1 && pin <= 3) {
		return false
	}

	if port == 4 && pin == 3 {
		return false
	}

	return true
}

func (e *Exporter) Run() error {
	defer close(e.stoppedChan)

	logFields := log.Fields{}
	if e.cfg.Bus != 0 {
		logFields["bus"] = e.cfg.Bus
	}
	if e.cfg.Device != 0 {
		logFields["device"] = e.cfg.Device
	}
	e.log = log.WithFields(logFields)

	dev := findIoBoard(e.cfg.Bus, e.cfg.Device)
	if dev == nil {
		e.log.Error("ioboard device not found")
		return configError
	}

	err := dev.Open()
	if err != nil {
		e.log.WithError(err).Error("device open error")
		return configError
	}

	e.dev = dev
	defer dev.Close()

	info := dev.Info()
	e.log = log.WithField("bus", info.Bus).WithField("device", info.Device)

	pullUpEnabled := false

	var pins []*pinData

	for _, c := range e.cfg.Pins {
		p := &pinData{log: e.log.WithField("port", c.Port).WithField("pin", c.Pin).WithField("name", c.Name)}
		pins = append(pins, p)

		portIdx := portIndex(c.Port)
		if portIdx < 0 {
			p.log.Error("invalid port")
			return configError
		}
		p.port = portIdx
		p.pin = c.Pin
		p.revert = c.Revert
		p.value = prometheus.NewGauge(prometheus.GaugeOpts{
			Name:        e.cfg.Prefix + "_" + c.Name,
			Help:        c.Help,
			ConstLabels: c.Labels,
		})
		prometheus.MustRegister(p.value)

		if !isPinAllowed(portIdx, c.Pin) {
			p.log.Error("invalid pin")
			return configError
		}

		if !isPinWired(portIdx, c.Pin) {
			p.log.Warn("pin is not wired")
		}

		if c.PullUp {
			if portIdx != 1 {
				p.log.Error("pull-up is not allowed")
				return configError
			}

			if !pullUpEnabled {
				pullUpEnabled = true

				_, err = e.setRegBit(regIntCon2, 7, 0)
				if err != nil {
					e.log.WithError(err).Error("error allowing pull-up on port b")
					return configError
				}
			}

			_, err = e.setRegBit(regWpuB, portIdx, 1)
			if err != nil {
				p.log.WithError(err).Error("error enabling pull-up")
				return configError
			}
		}

		_, err := e.setRegBit(regAnsel+portIdx, c.Pin, 0)
		if err != nil {
			p.log.WithError(err).Error("error setting port to digital")
			return configError
		}

		_, err = e.setRegBit(regTris+portIdx, c.Pin, 1)
		if err != nil {
			p.log.WithError(err).Error("error setting port to input")
			return configError
		}
	}

	for {
		for _, p := range pins {
			v, err := e.getRegBit(regPort+p.port, p.pin)
			if err != nil {
				p.log.Error("read error")
			}

			if v != 0 {
				v = 1
			}

			if p.revert {
				v ^= 1
			}

			p.log.WithField("value", v).Debug("read ok")

			p.value.Set(float64(v))
		}

		if utils.WaitFor(e.stopChan, time.Millisecond*time.Duration(e.cfg.ReadDelayMs)) {
			// stop requested
			return nil
		}
	}
}

func (e *Exporter) Stop() {
	close(e.stopChan)
	if !utils.WaitFor(e.stoppedChan, time.Second*30) {
		e.log.Warn("timeout on stop")
	}
}

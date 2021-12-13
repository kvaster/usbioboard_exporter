# usb-io-board exporter

This exporter provides prometheus compatible metrics from [usb io board](https://www.hardkernel.com/shop/usb-io-board/)
by hardkernel. Currently only gpio pins are supported.

## Supported pins

Usb io board is based on PIC18F45K50 chip from microchip. It has following gpio pins:

* RA: 0-7
* RB: 0-7
* RC: 0-2,6-7
* RD: 0-7
* RE: 0-3

But usb io board physically exposes only following:

* RA: 0-7
* RB: 0-5
* RC: 0-1,6-7
* RD: 0,4-7
* RE: 0-2

Internal pull up can be configured only on pins rb0-7. 

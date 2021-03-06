/*
Package dev ...

PMS7003 is the driver of PMS7003, an air quality sensor which can be used to detect PM2.5 and PM10.

Config Your Pi:
1. $ sudo vim /boot/config.txt
	add following new line:
	~~~~~~~~~~~~~~~~~
	enable_uart=1
	~~~~~~~~~~~~~~~~~
2. $ sudo vim /boot/cmdline.txt
	remove following contexts:
	~~~~~~~~~~~~~~~~~~~~~~~~~~
	console=serial0,115200
	~~~~~~~~~~~~~~~~~~~~~~~~~~
3. $ sudo reboot now
4. $ sudo cat /dev/ttyAMA0
	should see somethings output

Connect to Pi:
 - VCC: any 5v pin
 - GND: any gnd pin
 - TXD: must connect to pin 10(gpio 15) (RXD)
 - RXT: must connect to pin  8(gpio 14) (TXD)

*/
package dev

import (
	"fmt"
	"log"
	"math"

	"github.com/shanghuiyang/rpi-devices/base"
	"github.com/tarm/serial"
)

const (
	maxDeltaPM25 = 150
)

var (
	mockPMs       = []uint16{50, 110, 150, 110, 50}
	mockPMArryIdx = -1
)

// PMS7003 ...
type PMS7003 struct {
	port     *serial.Port
	buf      [128]byte
	history  *base.History
	maxRetry int
}

// NewPMS7003 ...
func NewPMS7003() *PMS7003 {
	p := &PMS7003{
		history:  base.NewHistory(10),
		maxRetry: 10,
	}
	if err := p.open(); err != nil {
		return nil
	}
	return p
}

// Get returns pm2.5 and pm10 in ug/m3
func (p *PMS7003) Get() (uint16, uint16, error) {
	for i := 0; i < p.maxRetry; i++ {
		if err := p.port.Flush(); err != nil {
			return 0, 0, err
		}
		a := 0
		for a < 32 {
			n, err := p.port.Read(p.buf[a:])
			if err != nil {
				// try to reopen serial
				p.port.Close()
				if err := p.open(); err != nil {
					log.Printf("[psm7003]failed open serial, error: %v", err)
				}
				return 0, 0, fmt.Errorf("error on read from port, error: %v. try to open serial again", err)
			}
			a += n
		}

		if a != 32 {
			log.Printf("[psm7003]incorrect data len: %v, expected 32", a)
			continue
		}
		if p.buf[0] != 0x42 && p.buf[1] != 0x4d && p.buf[2] != 0 && p.buf[3] != 28 {
			log.Printf("[psm7003]incorrect data in byte 1~4")
			continue
		}
		checksum := uint16(0)
		for i := 0; i < 29; i++ {
			checksum += uint16(p.buf[i])
		}
		if checksum != (uint16(p.buf[30])<<8)|uint16(p.buf[31]) {
			log.Printf("[psm7003]checksum failure")
			continue
		}

		pm25 := (uint16(p.buf[6]) << 8) | uint16(p.buf[7])
		pm10 := (uint16(p.buf[8]) << 8) | uint16(p.buf[9])
		if !p.checkDelta(pm25) {
			log.Printf("[psm7003]check delta failed, discard current data. pm2.5: %v", pm25)
			continue
		}
		return pm25, pm10, nil
	}
	return 0, 0, fmt.Errorf("psm7003 is invalid currently")
}

// Close ...
func (p *PMS7003) Close() {
	p.port.Close()
}

func (p *PMS7003) open() error {
	c := &serial.Config{Name: "/dev/ttyAMA0", Baud: 9600}
	port, err := serial.OpenPort(c)
	if err != nil {
		return err
	}
	p.port = port
	return nil
}

func (p *PMS7003) checkDelta(pm25 uint16) bool {
	avg, err := p.history.Avg()
	if err != nil {
		if err == base.ErrEmpty {
			p.history.Add(pm25)
			return true
		}
		return false
	}

	passed := math.Abs(avg-float64(pm25)) < maxDeltaPM25
	if passed {
		p.history.Add(pm25)
	}
	return passed
}

// Mock ...
func (p *PMS7003) Mock() (uint16, uint16, error) {
	mockPMArryIdx++
	if mockPMArryIdx == len(mockPMs) {
		mockPMArryIdx = 0
	}
	return mockPMs[mockPMArryIdx], mockPMs[mockPMArryIdx], nil
}

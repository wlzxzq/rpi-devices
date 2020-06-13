package main

import (
	"fmt"
	"log"
	"time"

	"github.com/shanghuiyang/rpi-devices/base"
	"github.com/shanghuiyang/rpi-devices/dev"
	"github.com/shanghuiyang/rpi-devices/iot"
	"github.com/stianeikeland/go-rpio"
)

const (
	pinLed = 26
)

func main() {
	if err := rpio.Open(); err != nil {
		log.Fatalf("failed to open rpio, error: %v", err)
		return
	}
	defer rpio.Close()

	temp := dev.NewDS18B20()
	led := dev.NewLed(pinLed)
	oled, err := dev.NewOLED(128, 32)
	if err != nil {
		log.Printf("failed to create an oled, error: %v", err)
		log.Printf("homeasst will work without oled")
	}

	wsnCfg := &base.WsnConfig{
		Token: base.WsnToken,
		API:   base.WsnNumericalAPI,
	}
	cloud := iot.NewCloud(wsnCfg)

	asst := newHomeAsst(temp, oled, led, cloud)
	base.WaitQuit(func() {
		asst.stop()
		rpio.Close()
	})
	asst.start()
}

type value struct {
	temp float32
	humi float32
}

type homeAsst struct {
	temp      *dev.DS18B20
	oled      *dev.OLED
	led       *dev.Led
	cloud     iot.Cloud
	chDisplay chan *value // for disploying on oled
	chCloud   chan *value // for pushing to iot cloud
	chAlert   chan *value // for alerting
}

func newHomeAsst(temp *dev.DS18B20, oled *dev.OLED, led *dev.Led, cloud iot.Cloud) *homeAsst {
	return &homeAsst{
		temp:      temp,
		oled:      oled,
		led:       led,
		cloud:     cloud,
		chDisplay: make(chan *value, 4),
		chCloud:   make(chan *value, 4),
		chAlert:   make(chan *value, 4),
	}
}

func (h *homeAsst) start() {
	go h.display()
	go h.push()
	go h.alert()
	h.getData()
}

func (h *homeAsst) getData() {
	for {
		temp, err := h.temp.GetTemperature()
		if err != nil {
			log.Printf("failed to get temperature, error: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}
		log.Printf("temp: %v", temp)

		v := &value{
			temp: temp,
			humi: -1,
		}
		h.chDisplay <- v
		h.chCloud <- v
		h.chAlert <- v
		time.Sleep(60 * time.Second)
	}
}

func (h *homeAsst) display() {
	var temp, humi float32 = -999, -999
	on := true
	for {
		select {
		case v := <-h.chDisplay:
			temp, humi = v.temp, v.humi
		default:
			// do nothing, just use the latest temp
		}

		if h.oled == nil {
			time.Sleep(30 * time.Second)
			continue
		}

		hour := time.Now().Hour()
		if hour >= 20 || hour < 8 {
			// turn off oled at 20:00-08:00
			if on {
				h.oled.Off()
				on = false
			}
			time.Sleep(10 * time.Second)
			continue
		}

		on = true
		tText := "--"
		if temp > -273 {
			tText = fmt.Sprintf("%.0f'C", temp)
		}
		if err := h.oled.Display(tText, 35, 0, 35); err != nil {
			log.Printf("display: failed to display temperature, error: %v", err)
		}
		time.Sleep(3 * time.Second)

		hText := "  --"
		if humi >= 0 {
			hText = fmt.Sprintf("%.0f%%", humi)
		}
		if err := h.oled.Display(hText, 35, 0, 35); err != nil {
			log.Printf("display: failed to display humidity, error: %v", err)
		}
		time.Sleep(3 * time.Second)
	}
}

func (h *homeAsst) push() {
	for v := range h.chCloud {
		go func(v *value) {
			tv := &iot.Value{
				Device: "5d3c467ce4b04a9a92a02343",
				Value:  v.temp,
			}
			if err := h.cloud.Push(tv); err != nil {
				log.Printf("push: failed to push temperature to cloud, error: %v", err)
			}

			hv := &iot.Value{
				Device: "5d3c4627e4b04a9a92a02342",
				Value:  v.humi,
			}
			if err := h.cloud.Push(hv); err != nil {
				log.Printf("push: failed to push humidity to cloud, error: %v", err)
			}
		}(v)
	}
}

func (h *homeAsst) alert() {
	var temp, humi float32 = -999, -999
	for {
		select {
		case v := <-h.chAlert:
			temp, humi = v.temp, v.humi
		default:
			// do nothing
		}

		if (temp > 0 && temp < 15) || humi > 70 {
			h.led.Blink(1, 1000)
			continue
		}
		time.Sleep(1 * time.Second)
	}
}

func (h *homeAsst) stop() {
	if h.oled != nil {
		h.oled.Close()
	}
	h.led.Off()
}

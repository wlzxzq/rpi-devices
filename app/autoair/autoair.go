/*
Auto-Air opens the air-cleaner automatically when the pm2.5 >= 130.
*/

package main

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"github.com/shanghuiyang/rpi-devices/base"
	"github.com/shanghuiyang/rpi-devices/dev"
	"github.com/shanghuiyang/rpi-devices/iot"
	"github.com/stianeikeland/go-rpio"
)

const (
	pinSG = 18
)

const (
	trigOnPM25  = 120
	trigOffPm25 = 100
)

var (
	autoair *autoAir
)

var bool2int = map[bool]int{
	false: 0,
	true:  1,
}

type pm25Response struct {
	PM25     uint16 `json:"pm25"`
	ErrorMsg string `json:"error_msg"`
}

type autoAir struct {
	sg      *dev.SG90
	cloud   iot.Cloud
	state   bool        // true: turn on, false: turn off
	chClean chan uint16 // for turning on/off the air-cleaner
	chCloud chan uint16 // for pushing to iot cloud
}

func main() {
	if err := rpio.Open(); err != nil {
		log.Fatalf("[autoair]failed to open rpio, error: %v", err)
		return
	}
	defer rpio.Close()

	sg := dev.NewSG90(pinSG)
	onenetCfg := &base.OneNetConfig{
		Token: base.OneNetToken,
		API:   base.OneNetAPI,
	}
	cloud := iot.NewCloud(onenetCfg)

	autoair = newAutoAir(sg, cloud)
	base.WaitQuit(func() {
		autoair.stop()
		rpio.Close()
	})
	autoair.start()
}

func newAutoAir(sg *dev.SG90, cloud iot.Cloud) *autoAir {
	return &autoAir{
		sg:      sg,
		cloud:   cloud,
		state:   false,
		chClean: make(chan uint16, 4),
		chCloud: make(chan uint16, 4),
	}
}

func (a *autoAir) start() {
	log.Printf("[autoair]service starting")
	go a.sg.Roll(45)
	go a.clean()
	a.detect()
}

func (a *autoAir) detect() {
	log.Printf("[autoair]detecting pm2.5")
	for {
		pm25, err := a.getPM25()
		if err != nil {
			log.Printf("[autoair]failed to get pm2.5, error: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}
		log.Printf("[autoair]pm2.5: %v ug/m3", pm25)

		a.chClean <- pm25
		time.Sleep(60 * time.Second)
	}
}

func (a *autoAir) clean() {
	go func() {
		for {
			time.Sleep(60 * time.Second)
			v := &iot.Value{
				Device: "air-cleaner",
				Value:  bool2int[a.state],
			}
			if err := a.cloud.Push(v); err != nil {
				log.Printf("[autoair]push: failed to push the state of air-cleaner to cloud, error: %v", err)
			}
		}
	}()

	for pm25 := range a.chClean {
		hour := time.Now().Hour()
		if pm25 < 400 && (hour >= 20 || hour < 8) {
			// disable at 20:00-08:00
			log.Printf("[autoair]auto air-cleaner was disabled at 20:00-08:00")
			if a.state {
				a.off()
			}
			continue
		}

		if !a.state && pm25 >= trigOnPM25 {
			a.on()
			log.Printf("[autoair]air-cleaner was turned on")
			continue
		}
		if a.state && pm25 < trigOffPm25 {
			a.off()
			log.Printf("[autoair]air-cleaner was turned off")
			continue
		}
	}
}

func (a *autoAir) getPM25() (uint16, error) {
	resp, err := http.Get("http://localhost:8000/pm25")
	if err != nil {
		log.Printf("[autoair]failed to get pm2.5 from sensers server, status: %v, err: %v", resp.Status, err)
		return 0, err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[autoair]failed to read resp body, err: %v", err)
		return 0, err
	}

	var pm25Resp pm25Response
	if err := json.Unmarshal(body, &pm25Resp); err != nil {
		log.Printf("[autoair]failed to unmarshal resp, err: %v", err)
		return 0, err
	}

	if pm25Resp.ErrorMsg != "" {
		log.Printf("[autoair]failed to get pm2.5 from sensers server, status: %v, err msg: %v", resp.Status, pm25Resp.ErrorMsg)
		return 0, err
	}

	return pm25Resp.PM25, nil
}

func (a *autoAir) on() {
	a.sg.Roll(0)
	time.Sleep(1 * time.Second)
	a.sg.Roll(-45)
	a.state = true
}

func (a *autoAir) off() {
	a.sg.Roll(0)
	time.Sleep(1 * time.Second)
	a.sg.Roll(45)
	a.state = false
}

func (a *autoAir) stop() {
	a.sg.Roll(45)
}

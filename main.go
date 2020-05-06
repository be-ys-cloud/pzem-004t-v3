package main

import (
	"fmt"
	"os"
	"time"

	"github.com/dark705/pzem-004t-v3/pzem"
)

const toPrint = `
Voltage: %f
Intensity: %f
Power: %f
Frequency: %f
Energy: %f
PowerFactor: %f
`

func main() {
	p, err := pzem.Setup(
		pzem.Config{
			Port:  os.Args[1],
			Speed: 9600,
			TimeOut: time.Second * 5,
		})
	if err != nil {
		panic(err)
	}

	err = p.ResetEnergy()
	if err != nil {
		panic(err)
	}

	t := time.NewTicker(1 * time.Second)
	for {
		<-t.C
		voltage, err := p.Voltage()
		if err != nil {
			panic(err)
		}
		intensity, err := p.Intensity()
		if err != nil {
			panic(err)
		}
		power, err := p.Power()
		if err != nil {
			panic(err)
		}
		frequency, err := p.Frequency()
		if err != nil {
			panic(err)
		}
		energy, err := p.Energy()
		if err != nil {
			panic(err)
		}
		powerFactor, err := p.PowerFactor()
		if err != nil {
			panic(err)
		}
		fmt.Printf(toPrint, voltage, intensity, power, frequency, energy, powerFactor)
	}
}

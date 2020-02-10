package main

import (
	"fmt"
	"log"
	"time"

	"github.com/d2r2/go-i2c"
	"github.com/d2r2/go-logger"
)

func main() {
	// Create new connection to I2C bus on 2 line with address 0x27
	logger.ChangePackageLogLevel("i2c", logger.InfoLevel)
	ms4525, err := i2c.NewI2C(0x28, 1)
	if err != nil {
		log.Fatal(err)
	}
	// Free I2C connection on exit
	defer ms4525.Close()

	//Start condition
	start := make([]byte, 0)
	data := make([]byte, 4)

	for {
		_, err = ms4525.ReadBytes(start)
		if err != nil {
			log.Fatal("Failed to send start conversion byte:", err)
		}
		time.Sleep(10 * time.Millisecond)

		_, err = ms4525.ReadBytes(data)
		if err != nil {
			log.Fatal("Failed to read data:", err)
		}

		pressure := float64(uint16(data[0])<<8 | uint16(data[1]))
		pmax := 1.0
		pmin := -1.0
		pressure = -((pressure-0.1*16383)*(pmax-pmin)/(0.8*16383) + pmin)
		//-((dp_raw - 0.1f*16383) * (P_max-P_min)/(0.8f*16383) + P_min);
		temp := float64(uint16(data[2])<<3 | uint16((data[3]&0b11100000)>>5))
		temp = ((200 * temp) / 2047) - 50
		//((200.0f * dT_raw) / 2047) - 50
		//fmt.Printf("One: %v, Two: %v, Three: %v\n", strconv.FormatUint(uint64(data[0]), 2), strconv.FormatUint(uint64(data[1]), 2), strconv.FormatUint(uint64(data[2]), 2))
		fmt.Printf("Pressure: %v, Temp: %v\n", pressure, temp)
	}
}

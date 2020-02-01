package hydra

import (
	"fmt"
	"log"
	"time"

	"go.bug.st/serial"

	"github.com/slim-bean/leafbus/pkg/push"
)

const (
	binaryCmd = ":b\r"
	maxPacket = 71 // 7 + 4 * (max batch == 64)
)

type packet struct {
	*packetType
	address  byte
	data     []byte
	checksum uint16
}

type packetType struct {
	hasData       bool
	isBatch       bool
	batchLen      int
	batchLength   byte
	commandFailed bool
}

func read(bytes []byte) (*packet, error) {

	p := &packet{}

	p.packetType = parsePacketType(bytes[0])

	if len(bytes) < 4+p.batchLen*4 {
		return nil, fmt.Errorf("packet is not long enough.  Expected: %v, received %v", 7+len(p.data), len(bytes))
	}

	p.address = bytes[1]
	p.data = make([]byte, p.batchLen*4)
	if p.hasData {
		for i := 0; i < p.batchLen*4; i++ {
			p.data[i] = bytes[2+i]
		}
	}

	/*
		The checksum information isn't defined in the doc, looking at another doc from the same manufacturer for a different
		part which uses the same serial interface, it looks like it's supposed to be the sum of all the bytes starting with
		the snp start new packet bytes, however when I look at what comes over the bus, all I ever see are 6 and 6 for the
		2 checksum bytes and it never changes regardless of the values....

		This is what I used originally to calc the checksum, not sure why i'm leaving it but I am :)

		recvChecksum := uint16(bytes[2+p.dataLen])<<8 | uint16(bytes[2+p.dataLen+1])

		var calcChecksum uint16
		calcChecksum = 's' + 'n' + 'p'
		for i := 0; i < p.dataLen; i++ {
			calcChecksum += uint16(bytes[i])
		}
	*/

	//if bytes[2+p.batchLen*4] != 6 && bytes[2+p.batchLen*4+1] != 6 {
	//	var calcChecksum uint16
	//	calcChecksum = 's' + 'n' + 'p'
	//	for i := 0; i < p.batchLen*4; i++ {
	//		calcChecksum += uint16(bytes[i])
	//	}
	//	recvChecksum := uint16(bytes[2+p.batchLen*4])<<8 | uint16(bytes[2+p.batchLen*4+1])
	//	return nil, fmt.Errorf("received unexpected checksum: %v, calculated: %v", recvChecksum, calcChecksum)
	//}

	return p, nil

}

func parsePacketType(b byte) *packetType {
	p := &packetType{}

	if b&0b10000000 == 0b10000000 {
		p.hasData = true
	} else {
		p.hasData = false
	}

	if b&0b01000000 == 0b01000000 {
		p.isBatch = true
	} else {
		p.isBatch = false
	}

	if b&0b00000001 == 0b00000001 {
		p.commandFailed = true
	} else {
		p.commandFailed = false
	}

	if p.isBatch {
		bl := (b & 0b00111100) >> 2
		p.batchLen = int(bl)
	} else {
		p.batchLen = 1
	}
	return p
}

type state int

const (
	unknown state = iota
	hb1
	hb2
	pt
	address
	data
	chksum1
	chksum2
)

type hydra struct {
	handler        *push.Handler
	p              serial.Port
	sendChan       chan *packet
	currState      state
	currDataRemain int
	currPacket     []byte
}

func NewHydra(handler *push.Handler, port string) (*hydra, error) {
	mode := &serial.Mode{}
	p, err := serial.Open(port, mode)
	if err != nil {
		return nil, err
	}

	h := &hydra{
		handler:   handler,
		p:         p,
		sendChan:  make(chan *packet),
		currState: unknown,
	}
	go h.receive()
	go h.send()

	return h, nil
}

func (h *hydra) EnterBinaryMode() error {
	_, err := h.p.Write([]byte(binaryCmd))
	return err
}

func (h *hydra) processByte(b byte) {
	if h.currState == unknown && b == 's' {
		h.currState = hb1
		return
	}
	if h.currState == hb1 && b == 'n' {
		h.currState = hb2
		return
	}
	if h.currState == hb2 && b == 'p' {
		h.currState = pt
		return
	}

	// If we got screwed up somewhere eventually the packet will be too long so reset the state and start again
	if h.currState >= pt && len(h.currPacket) > maxPacket {
		log.Println("packet got screwed up, exceeded max length")
		h.currState = unknown
		h.currDataRemain = 0
		h.currPacket = h.currPacket[:0]
		return
	}

	if h.currState == pt {
		pt := parsePacketType(b)
		h.currPacket = append(h.currPacket, b)
		h.currState = address
		if pt.hasData {
			h.currDataRemain = pt.batchLen * 4
		} else {
			h.currDataRemain = 0
		}
		return
	}

	if h.currState == address {
		h.currPacket = append(h.currPacket, b)
		h.currState = data
		return
	}

	if h.currState == data && h.currDataRemain > 0 {
		h.currPacket = append(h.currPacket, b)
		h.currDataRemain--
		if h.currDataRemain == 0 {
			h.currState = chksum1
		}
		return
	}

	if h.currState == chksum1 {
		h.currPacket = append(h.currPacket, b)
		h.currState = chksum2
	}

	if h.currState == chksum2 {
		h.currPacket = append(h.currPacket, b)
		pkt, err := read(h.currPacket)
		if err != nil {
			log.Println(err)
		} else {
			h.sendChan <- pkt
		}

		h.currState = unknown
		h.currDataRemain = 0
		h.currPacket = h.currPacket[:0]
	}

}

func (h *hydra) send() {
	for {
		select {
		case p := <-h.sendChan:
			if p.address != 0x55 {
				log.Println("Received unexpected address from Hydra, ignoring")
				continue
			}
			// The status packet should be 4 bytes
			if p.batchLen != 4 {
				log.Println("Received unexpected number of bytes in payload, ignoring")
				continue
			}
			t := time.Now()
			v1Current := uint16(p.data[0]&0b00001111)<<8 | uint16(p.data[1])
			v1Volts := uint16(p.data[2])<<8 | uint16(p.data[3])
			v2Current := uint16(p.data[4]&0b00001111)<<8 | uint16(p.data[5])
			v2Volts := uint16(p.data[6])<<8 | uint16(p.data[7])
			v3Current := uint16(p.data[8]&0b00001111)<<8 | uint16(p.data[9])
			v3Volts := uint16(p.data[10])<<8 | uint16(p.data[11])
			vInVolts := uint16(p.data[14])<<8 | uint16(p.data[15])

			if h.handler != nil {
				h.handler.SendMetric("v1_volts", nil, t, float64(v1Volts)/1000)
				h.handler.SendMetric("v1_amps", nil, t, float64(v1Current)/1000)
				h.handler.SendMetric("v2_volts", nil, t, float64(v2Volts)/1000)
				h.handler.SendMetric("v2_amps", nil, t, float64(v2Current)/1000)
				h.handler.SendMetric("v3_volts", nil, t, float64(v3Volts)/1000)
				h.handler.SendMetric("v3_amps", nil, t, float64(v3Current)/1000)
				h.handler.SendMetric("vin_volts", nil, t, float64(vInVolts)/1000)
			} else {
				log.Println(v1Current, v1Volts, v2Current, v2Volts, v3Current, v3Volts, vInVolts)
			}
		}
	}
}

func (h *hydra) receive() {
	buff := make([]byte, 100)
	for {
		time.Sleep(20 * time.Millisecond)
		n, err := h.p.Read(buff)
		if err != nil {
			log.Fatal(err)
		}
		if n == 0 {
			continue
		}
		//log.Printf("%v", buff[:n])
		for i := 0; i < n; i++ {
			h.processByte(buff[i])
		}
	}
}

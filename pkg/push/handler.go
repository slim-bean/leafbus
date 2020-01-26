package push

import (
	"log"
	"time"

	"github.com/brutella/can"
	"github.com/prometheus/prometheus/pkg/labels"
)

const (
	name = "__name__"
)

type Handler struct {
	cortex *Cortex
}

func NewHandler(cortex *Cortex) (*Handler, error) {
	h := &Handler{
		cortex: cortex,
	}
	return h, nil
}

func (h *Handler) Handle(frame can.Frame) {
	// Only care about current charge status
	if frame.ID != 0x55B {
		return
	}
	if len(h.cortex.data) >= 100 {
		log.Println("Ignoring packet, send buffer is full")
		return
	}

	currCharge := (uint16(frame.Data[0]) << 2) | (uint16(frame.Data[1]) >> 6)

	p := packetPool.Get().(*packet)
	p.sample.TimestampMs = int64(time.Now().Nanosecond() / 1000 * 1000)
	p.sample.Value = float64(currCharge)
	l := labelsPool.Get().(labels.Label)
	l.Name = name
	l.Value = "curent_soc"
	p.labels = append(p.labels, l)

	h.cortex.data <- p

}

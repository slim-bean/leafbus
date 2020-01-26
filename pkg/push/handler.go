package push

import "github.com/brutella/can"

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
	currCharge := (uint16(frame.Data[0]) << 2) | (uint16(frame.Data[1]) >> 6)

	// parse the packet and send on a channel the labels and value
}

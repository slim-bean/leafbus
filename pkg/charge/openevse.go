package charge

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type chargeState int

const (
	unknown chargeState = iota
	sleeping
	charging
)

func (c chargeState) String() string {
	return [...]string{"UNKNOWN", "SLEEPING", "CHARGING"}[c]
}

func parseChargerResponse(in *response) (chargeState, error) {
	switch in.Cmd {
	case "$GS":
		{
			parts := strings.Split(in.Ret, " ")
			if len(parts) != 3 {
				return unknown, errors.New("response did not have the expected number of parts")
			}
			if parts[0] != "$OK" {
				return unknown, fmt.Errorf("response was not $OK, was: %v", parts[0])
			}
			switch parts[1] {
			case "254":
				return sleeping, nil
			case "3":
				return charging, nil
			}
		}
	case "$FS":
		{
			parts := strings.Split(in.Ret, " ")
			if len(parts) == 0 {
				return unknown, errors.New("response did not have the expected number of parts")
			}
			if !strings.HasPrefix(parts[0], "$OK") {
				return unknown, fmt.Errorf("response was not $OK, was: %v", parts[0])
			}
			return sleeping, nil
		}
	}
	return 0, fmt.Errorf("unknown response command: %v", in.Cmd)
}

type command int

const (
	query command = iota
	sleep
)

func (s command) command() string {
	return [...]string{"$GS", "$FS"}[s]
}

type response struct {
	Cmd string `json:"cmd"`
	Ret string `json:"ret"`
}

type openevse struct {
	client  *http.Client
	baseURL *url.URL
}

func newopenevse(address string) (*openevse, error) {
	u, err := url.Parse(address)
	if err != nil {
		return nil, err
	}
	o := &openevse{
		client:  http.DefaultClient,
		baseURL: u,
	}
	return o, nil
}

func (o *openevse) sendCommand(c command) (chargeState, error) {
	u := o.baseURL
	u.Path = "/r"
	v := o.baseURL.Query()
	v.Set("json", "1")
	v.Set("rapi", c.command())
	u.RawQuery = v.Encode()
	//log.Println("Query:", u.String())
	resp, err := o.client.Get(u.String())
	if err != nil {
		return unknown, err
	}
	var r response
	err = json.NewDecoder(resp.Body).Decode(&r)
	if err != nil {
		return unknown, err
	}
	return parseChargerResponse(&r)
}

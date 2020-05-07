package playback

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func bounds(r *http.Request) (time.Time, time.Time, error) {
	start, err := parseTimestamp(r.Form.Get("start"))
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	end, err := parseTimestamp(r.Form.Get("end"))
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	return start, end, nil
}

// parseUnixNano parses a ns unix timestamp from a string
// if the value is empty it returns a default value passed as second parameter
func parseTimestamp(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, fmt.Errorf("missing start or end")
	}

	if strings.Contains(value, ".") {
		if t, err := strconv.ParseFloat(value, 64); err == nil {
			s, ns := math.Modf(t)
			ns = math.Round(ns*1000) / 1000
			return time.Unix(int64(s), int64(ns*float64(time.Second))), nil
		}
	}
	nanos, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		if ts, err := time.Parse(time.RFC3339Nano, value); err == nil {
			return ts, nil
		}
		return time.Time{}, err
	}
	if len(value) <= 10 {
		return time.Unix(nanos, 0), nil
	}
	return time.Unix(0, nanos*1e6), nil
}

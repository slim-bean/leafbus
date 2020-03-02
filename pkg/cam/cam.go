package cam

import (
	"encoding/base64"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/prometheus/prometheus/pkg/labels"

	"github.com/slim-bean/leafbus/pkg/push"
)

const (
	imageFile = "/dev/shm/img.jpg"
)

type Cam struct {
	cmd     *exec.Cmd
	handler *push.Handler
}

func NewCam(handler *push.Handler) (*Cam, error) {
	cmd := exec.Command("/usr/bin/raspistill", "--signal", "--encoding", "jpg", "-w", "1280", "-h", "960", "-o", imageFile)
	c := &Cam{
		cmd:     cmd,
		handler: handler,
	}
	go c.trigger()
	go c.read()
	return c, nil
}

func (c *Cam) trigger() {
	log.Println("Running raspistill")

	err := c.cmd.Start()
	if err != nil {
		log.Println("Failed to start raspistill:", err)
		return
	}

	for {
		time.Sleep(5 * time.Second)
		//log.Println("Sending signal to capture image")

		err = c.cmd.Process.Signal(syscall.SIGUSR1)
		if err != nil {
			log.Println("Error sending SIGUSR1:", err)
			kill(c.cmd)
			return
		}
	}

}

func (c *Cam) read() {
	ticker := time.NewTicker(1 * time.Second)
	for {
		select {
		case <-ticker.C:

			_, err := os.Stat(imageFile)
			if err != nil && os.IsNotExist(err) {
				continue
			} else if err != nil {
				log.Println("Failed to stat camera file:", err)
				continue
			}
			bytes, err := ioutil.ReadFile(imageFile)
			if err != nil {
				log.Println("Failed to read file:", err)
				_ = os.Remove(imageFile)
				continue
			}

			lbls := labels.Labels{
				labels.Label{
					Name:  "job",
					Value: "camera",
				},
			}
			log.Println("Found image")

			c.handler.SendLog(lbls, time.Now(), base64.StdEncoding.EncodeToString(bytes))

			err = os.Remove(imageFile)
			if err != nil {
				log.Println("Failed to delete image file after uploade:", err)
			}

		}
	}

}

func kill(c *exec.Cmd) {
	err := c.Process.Kill()
	if err != nil {
		log.Println("Error killing the raspistill process!, it may still be running: ", err)
	}
}

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

var (
	camLabels = labels.Labels{
		labels.Label{
			Name:  "job",
			Value: "camera",
		},
	}
)

type Cam struct {
	cmd       *exec.Cmd
	handler   *push.Handler
	runChan   chan bool
	shouldRun bool
}

func NewCam(handler *push.Handler) (*Cam, error) {

	c := &Cam{
		cmd:       nil,
		handler:   handler,
		runChan:   make(chan bool),
		shouldRun: false,
	}
	go c.trigger()
	go c.read()
	return c, nil
}

func (c *Cam) Start() {
	c.runChan <- true
}

func (c *Cam) Stop() {
	c.runChan <- false
}

func (c *Cam) trigger() {
	log.Println("Running raspistill")

	//err := c.cmd.Start()
	//if err != nil {
	//	log.Println("Failed to start raspistill:", err)
	//	return
	//}

	ticker := time.NewTicker(5 * time.Second)

	for {
		select {
		case r := <-c.runChan:
			c.shouldRun = r
			if r == false {
				c.killCamera()
			} else {
				c.startCamera()
			}
		case <-ticker.C:
			if !c.shouldRun {
				continue
			}
			err := c.cmd.Process.Signal(syscall.SIGUSR1)
			if err != nil {
				log.Println("Error sending SIGUSR1:", err)
				c.killCamera()
				return
			}
		}
	}

}

func (c *Cam) read() {
	ticker := time.NewTicker(1 * time.Second)
	for {
		select {
		case <-ticker.C:
			if !c.shouldRun {
				continue
			}
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

			log.Println("Found image")

			c.handler.SendLog(camLabels, time.Now(), base64.StdEncoding.EncodeToString(bytes))

			err = os.Remove(imageFile)
			if err != nil {
				log.Println("Failed to delete image file after uploade:", err)
			}

		}
	}

}

func (c *Cam) startCamera() {
	if c.cmd != nil {
		return
	}
	cmd := exec.Command("/usr/bin/raspistill", "--signal", "--encoding", "jpg", "-w", "640", "-h", "480", "-o", imageFile)
	err := cmd.Start()
	if err != nil {
		log.Println("Failed to start raspistill:", err)
		return
	}
	c.cmd = cmd

}

func (c *Cam) killCamera() {
	if c.cmd == nil || c.cmd.Process == nil {
		return
	}
	err := c.cmd.Process.Kill()
	if err != nil {
		log.Println("Error killing the raspistill process!, it may still be running: ", err)
	}
	_ = c.cmd.Wait()
	c.cmd = nil
}



# leafbus

This is an app I built to capture CAN bus data and some other sensor data on my Nissan Leaf to help better understand
energy usage, give feedback while driving to encourage better driving habits, and help plan my driving routes.

This is very much a work in progress.


## Hardware

* Raspberry Pi 3B+ 
* [PiCan2 Duo](https://copperhilltech.com/pican2-duo-can-bus-board-for-raspberry-pi/)
* [GPS Sensor](https://www.adafruit.com/product/746)
* [Air Pressure](https://www.amazon.com/gp/product/B07FN6615W/ref=ppx_yo_dt_b_search_asin_title?ie=UTF8&psc=1) See notes below
* [Power Supply](http://www.chrobotics.com/shop/hydra) See notes below

### Notes 

**Power** 

I bought the PiCan2 Duo with the built in DC-DC converter hoping to use it to power everything.  I should have known when buying it the specified max output current of 1A wouldn't be enough, and it's not.
Save a few bucks and by the PiCan2 Duo without the DC-DC converter and get a separate supply.  The Hydra is just something I had laying around and sadly you can't buy them anymore.

I will try to find a recommendation for a PS at some point, anything that can handle an automotive input and deliver a couple amps, even more if you are using a Raspberry Pi 4

**Air Pressure**

I bought this sensor thinking it would be a good way to measure how the air hitting the car affects energy usage, does driving into the wind or away matter much?  Does "drafting" matter, etc.

So far I'm not sure it's worth it.  Also I would just get the ms4525 sensor for less money somewhere else.

## Raspberry Pi Setup

`/boot/config.txt`

```
[all]
dtoverlay=disable-bt
dtoverlay=mcp2515-can0,oscillator=16000000,interrupt=25
dtoverlay=mcp2515-can1,oscillator=16000000,interrupt=24
dtoverlay=spi-bcm2835-overlay
```

`sudo raspi-config`

```
Interface Options->I2C->Enable
```


## Building Dependencies 

This project needs Loki, Cortex, and the Grafana Cloud Agent all running on the Raspberry Pi, here's how to build them

To cross compile the docker images for Cortex and Loki I use [buildx](https://docs.docker.com/buildx/working-with-buildx/)

As of may 2020 I still have to do this after every reboot to get a builder that will cross compile to the armv7 architecture

```bash
docker buildx rm builder
docker run --rm --privileged multiarch/qemu-user-static --reset -p yes
docker buildx create --name builder --driver docker-container --use
docker buildx inspect --bootstrap
```

### Prebuilt docker images:

slimbean/cortex-amd:latest
slimbean/agent-arm:latest
slimbean/loki-arm:latest

### Loki

The Loki project already exports armv7 images which run on Raspbian, feel free to use a recent version.  
I only have one change which probably won't be in the 1.5.0 release, which allows setting the encoding type to None for the chunks.
This is something I'm playing around with when storing images (which are already compressed) inside Loki but you don't need to have it.
Using `snappy` works well too. 

In the root of the Loki project run, modify the push target as necessary:

```bash
docker buildx build --build-arg "TOUCH_PROTOS=1" --platform linux/amd64,linux/arm/v7 -f cmd/loki/Dockerfile --push -t slimbean/loki .
```

This takes a long time on my computer, an hour.... Buildkit is slow but it shouldn't be this slow, this has to be something with Loki.

### Cortex

Cortex does not build arm images so you'll need build the docker image yourself.

The native dockerfile isn't terribly friendly for this so I have a branch in my fork which helps:

```bash
git clone https://github.com/slim-bean/cortex
git checkout -b cortex-arm-docker-only origin/cortex-arm-docker-only
docker buildx build --platform linux/arm/v7 -t slimbean/cortex:latest -f cmd/cortex/Dockerfile --push .
```

Make sure to update that tag repo to your own for pushing.

If you are going to run a local cortex, you will want a different branch of that fork which lets cortex query the store for labels and also fixes an issue in Thanos

```bash
git checkout -b query-store-labels origin/query-store-labels
make all
```



### Grafana Cloud Agent

```bash
docker buildx build --platform linux/arm/v7 -t slimbean/agent:latest -f cmd/agent/Dockerfile --push .
```

### Grafana

I forked Grafana to hack on the Loki datasource, specifically to be able to send an `interval` to Loki which is required
when using it to store metrics style data.  I don't think I'm going to open a PR to merge this because it's not a very good solution.
Please see this [github issue](https://github.com/grafana/loki/issues/1779) for a better discussion about this problem.

I think I'd like to see this solved in LogQL and probably remove "interval" from Loki which is why I don't want to merge this change in Grafana.

Building Grafana to run on the Raspberry Pi is difficult... You'll want to use my fork/branch:

```bash
git clone https://github.com/slim-bean/grafana
git checkout -b loki-interval origin/loki-interval
make deps
make build-js
make build-server
make build-docker-dev
```

You'll need to hack up the makefile to change the build-docker-dev to push to your dockerhub


You'll also want to build a local docker which is not arm, this is easier:

```bash
make build-docker-full
```

I also hacked up that target to change the tag, you'll want to change that as well.

### Grafana Datasources and Panels

image-viewer-panel
streaming-json-datasource
grafana-trackmap-panel

## Running

### Raspberry Pi

Use official raspbian image, this makes things like the camera and using serial ports and things a lot easier but has one big drawback.

It's a 32bit OS and Cortex using the TSDB blocks store does not like 32bit OS, it will memory map TSDB blocks that are created.  
You will get out of memory errors if you use the compactor, and you will likely see other out of memory errors if you keep too much data on the Raspberry Pi.

I did at one point have 64bit Ubuntu installed however this made everything else harder so I moved away from it. 

Install Docker, Enable the Camera, Setup PiCan2 Duo

Run ansible playbook

There is a docker-compose file to get all the images running
```bash
`export DOCKER_HOST="ssh://pi@leaf.edjusted.com"`
`docker-compose up -d`
```
  
High level goals:

1. Use live streaming to influence better driving behaviors
2. Use stored data to determine more efficient routes

Challenges:

Eliminating variables






Hydra PS

`sudo picocom /dev/ttyUSB0`

`:x` enter to exit binary mode


Ras pi, ubuntu and serial

sudo apt remove snapd
sudo systemctl stop unattended-upgrades.service
sudo systemctl disable unattended-upgrades.service


Had to change the bootloader because the ubuntu uboot seems to enable the serial console via terminal and has a `Hit any key to stop autoboot` which would read the NMEA messages from the GPS module and stop booting.  I found [instructions here](https://wiki.ubuntu.com/ARM/RaspberryPi#Change_the_bootloader) but basically I only needed to modify the `/boot/firmware/config.txt` like so:

config.txt
```
[all]
arm_64bit=1
#device_tree_address=0x03000000
kernel=vmlinuz
initramfs initrd.img followkernel
```

And then we need to disable serial console from the OS too:

nobtcfg.txt
```
enable_uart=1
#cmdline=nobtcmd.txt
cmdline=nobtnoserialcmd.txt
dtoverlay=pi3-disable-bt
```

nobtnoserialcmd.txt
```
net.ifnames=0 dwc_otg.lpm_enable=0 console=tty1 root=LABEL=writable rootfstype=ext4 elevator=deadline rootwait fixrtc
```

(just removing `console=ttyAMA0,115200`)

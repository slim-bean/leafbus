

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

## Storage

Leafbus stores data locally using DuckDB and hourly Parquet files. Run `leafbus` with `--parquet-dir` (and optionally `--duckdb-path`) to set where data is written.

## Cross-compiling for ARM64

DuckDB uses CGO and links against `libstdc++`. When cross-compiling, install the ARM64 C++ toolchain and build with CGO enabled.

```bash
sudo apt-get update
sudo apt-get install gcc-aarch64-linux-gnu g++-aarch64-linux-gnu
```

Then build for arm64:

```bash
ARM_CC=aarch64-linux-gnu-gcc make arm
```

### Raspberry Pi runtime dependencies

If you see errors like:

- `GLIBC_2.32` / `GLIBC_2.34` / `GLIBC_2.38` not found
- `GLIBCXX_3.4.29` / `GLIBCXX_3.4.30` not found
- `CXXABI_1.3.13` not found

then the binary was linked against newer glibc/libstdc++ than the Pi provides. You have two options:

1) **Build on the Pi** (recommended for compatibility)

```bash
sudo apt-get update
sudo apt-get install build-essential
go build -o leafbus ./cmd/leafbus/main.go
```

2) **Upgrade the Pi's runtime** to a newer distro that ships newer glibc/libstdc++ (e.g. Debian bookworm or newer).

These errors indicate missing **glibc** and **libstdc++** versions on the target system.

## Grafana `/query` API

Leafbus exposes a read-only SQL endpoint at `POST /query`. It accepts only `SELECT` or `WITH` statements and returns JSON in this format:

```json
{
  "columns": ["ts", "value"],
  "rows": [
    ["2026-01-23T22:00:00Z", 42.0],
    ["2026-01-23T22:00:10Z", 43.1]
  ]
}
```

## Running

### Raspberry Pi

I'm running on a Raspberry Pi 4, if you are using something else the config.txt stuff may change

I installed a normal 64 bit Raspbery Pi OS

To enable support for the CAN board and an additional UART for the GPS module

```
[all]

# CAN board settings
dtoverlay=mcp2515-can0,oscillator=16000000,interrupt=25
dtoverlay=mcp2515-can1,oscillator=16000000,interrupt=24
dtoverlay=spi-bcm2835-overlay

# this may not strictly be necessary but AI tells me it locks the VPU clock which stabilizes the UARTS
enable_uart=1

# Will be used for GPS
dtoverlay=uart3
```

Then I created a systemd unit to init CAN `can-setup.service`

```
[Unit]
Description=CAN Bus Interface Setup

[Service]
Type=oneshot
ExecStart=/bin/ip link set can0 up type can bitrate 500000
ExecStart=/bin/ip link set can1 up type can bitrate 500000
User=root
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
```

and for the leafbus process `leafbus.service`
```
[Unit]
Description=Leafbus CAN Service
After=can-setup.service
Requires=can-setup.service

[Service]
Type=simple
ExecStartPre=/bin/sleep 10
ExecStart=/home/pi/leafbus
WorkingDirectory=/home/pi
User=root
Restart=always
StandardOutput=append:/home/pi/leafbus.out
StandardError=append:/home/pi/leafbus.out

[Install]
WantedBy=multi-user.target
```

##### Bluetooth

```
pi@leaf:~ $ rfkill list
0: hci0: Bluetooth
        Soft blocked: yes
        Hard blocked: no
1: phy0: Wireless LAN
        Soft blocked: no
        Hard blocked: no
```
`sudo rfkill unblock bluetooth`

##### Test GPS

```
pi@leaf:~ $ stty -F /dev/ttyAMA1 9600 raw
stty: /dev/ttyAMA1: No such file or directory
pi@leaf:~ $ stty -F /dev/ttyAMA3 9600 raw
pi@leaf:~ $ cat /dev/ttyAMA3
10,45,44,26,230,42*7D
$GPGSV,3,3,12,09,24,071,33,17,16,124,47,04,12,042,31,29,05,306,25*79
$GPRMC,003231.000,A,4301.0796,N,07741.7006,W,0.01,307.12,240126,,,D*75
$GPVTG,307.12,T,,M,0.01,N,0.02,K,D*3C
$GPGGA,003232.000,4301.0796,N,07741.7006,W,2,11,0.79,201.6,M,-34.3,M,0000,0000*55
$GPGSA,A,3,04,05,12,17,19,09,06,11,25,21,29,,1.44,0.79,1.21*03
$GPRMC,003232.000,A,4301.0796,N,07741.7006,W,0.01,251.59,240126,,,D*7B
$GPVTG,251.59,T,,M,0.01,N,0.01,K,D*32
$GPGGA,003233.000,4301.0796,N,07741.7006,W,2,11,0.79,201.7,M,-34.3,M,0000,0000*55
$GPGSA,A,3,04,05,12,17,19,09,06,11,25,21,29,,1.44,0.79,1.21*03
$GPRMC,003233.000,A,4301.0796,N,07741.7006,W,0.01,207.86,240126,,,D*7B
$GPVTG,207.86,T,,M,0.01,N,0.02,K,D*30
$GPGGA,003234.000,4301.0796,N,07741.7006,W,2,11,0.79,201.7,M,-34.3,M,0000,0000*52
$GPGSA,A,3,04,05,12,17,19,09,06,11,25,21,29,,1.44,0.79,1.21*03
$GPRMC,003234.000,A,4301.0796,N,07741.7006,W,0.02,122.54,240126,,,D*74
$GPVTG,122.54,T,,M,0.02,N,0.04,K,D*3E
```

##### Hydra PS

`sudo picocom /dev/ttyUSB0`

`:x` enter to exit binary mode


### Grafana JSON API datasource

Use the JSON API datasource plugin and configure:

- **URL**: `http://<leafbus-host>:7777`
- **HTTP Method**: `POST`
- **Path**: `/query`
- **Body**:
```json
{ "sql": "select ts, value from runtime_metrics where name = 'speed_mph' and ts >= to_timestamp(${__from}/1000) and ts <= to_timestamp(${__to}/1000) order by ts", "limit": 10000 }
```

### Example queries

Runtime metrics (time series):

```sql
select ts, value
from runtime_metrics
where name = 'speed_mph'
  and ts >= to_timestamp(${__from}/1000)
  and ts <= to_timestamp(${__to}/1000)
order by ts
```

Status table (latest 12V battery):

```sql
select ts, battery12v_volts, battery12v_soc, battery12v_amps, battery12v_temp_c
from status_hourly
where ts >= to_timestamp(${__from}/1000)
  and ts <= to_timestamp(${__to}/1000)
order by ts desc
limit 200
```

## Legacy Loki/Cortex Notes

These Loki/Cortex build notes are kept for historical reference and are no longer required for current data capture.

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


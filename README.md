

Docker 19.03.5

Still needed a newer builder

```
docker buildx create --name bld
docker buildx ls
docker buildx inspect bld --bootstrap
docker buildx use bld
```

```
 docker buildx build --platform linux/arm/v7 -t slimbean/cortex:latest -f cmd/cortex/Dockerfile --push .
 docker build -t slimbean/cortex-querier:latest -f cmd/cortex/Dockerfile .
```

`export DOCKER_HOST="ssh://ubuntu@leaf.edjusted.com"`

`docker-compose pull leafbus`
`docker-compose up -d --force-recreate`



```yaml
auth_enabled: false

server:
  http_listen_port: 8002
  grpc_listen_port: 9002

ingester:
  max_transfer_retries: 1

  lifecycler:
    # We want to start immediately.
    join_after: 0
    claim_on_rollout: false
    final_sleep: 0s
    num_tokens: 512
    address: 127.0.0.1
    ring:
      kvstore:
        store: inmemory
      replication_factor: 1


tsdb:
  dir: /srv/cortex-tsdb-ingester
  ship_interval: 1m
  block_ranges_period: [ 5m ]
  retention_period: 72h
  backend: s3

  bucket_store:
    sync_dir: /srv/cortex-tsdb-querier

  s3:
    endpoint:          minio:9000
    bucket_name:       cortex-tsdb
    access_key_id:     cortex
    secret_access_key: supersecret
    insecure:          true

storage:
  engine: tsdb
```

```yaml
auth_enabled: false

server:
  http_listen_port: 8003
  grpc_listen_port: 9003

ingester:
  lifecycler:
    address: 127.0.0.1
    ring:
      kvstore:
        store: inmemory
      replication_factor: 1
    final_sleep: 0s
  chunk_idle_period: 20m  # Set longer than max_chunk_age so that everything gets flushed at 15m
  chunk_retain_period: 30s
  max_transfer_retries: 0
  chunk_target_size: 1048576 # 1MB
  max_chunk_age: 15m

schema_config:
  configs:
  - from: 2018-04-15
    store: inmemory
    object_store: filesystem
    schema: v11
    index:
      prefix: index_
      period: 168h

storage_config:
  filesystem:
    directory: /var/lib/loki/chunks
```

http://cortex:8002/api/prom


  - [x] power supply (read input voltage and output current)
  - [ ] configure low voltage shutoff
  - [x] experiment with retention period settings on TSDB
  - [x] fix volume mounts
  - [x] gomadvdebug on cortex
  - [ ] scrape metrics and push them into local cortex without running prom
  - [ ] node-exporter
  
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

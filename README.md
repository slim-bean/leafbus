

Docker 19.03.5

Still needed a newer builder

```
docker buildx create --name nondefaultbuilder
docker buildx ls
docker buildx inspect nondefaultbuilder --bootstrap
docker buildx use nondefaultbuilder
```

`export DOCKER_HOST="ssh://ubuntu@leaf.edjusted.com"`

`docker-compose pull leafbus`
`docker-compose up -d --force-recreate`



```yaml
auth_enabled: false

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

http://cortex:8002/api/prom


  - [ ] power supply (read input voltage and output current)
  - [ ] configure low voltage shutoff
  - [ ] experiment with retention period settings on TSDB
  - [x] fix volume mounts
  - [x] gomadvdebug on cortex 

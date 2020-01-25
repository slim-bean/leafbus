

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

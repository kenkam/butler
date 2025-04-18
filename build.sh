#!/usr/bin/env bash

if [ -z "$1" ]; then
  echo "error: tag is required"
  exit 2
fi

go test || { exit 1; } 

git tag $1 || { exit 1; }

image=ghcr.io/kenkam/butler:$(git describe --abbrev=0 --tags | cut -d "v" - -f2)

docker build . -t $image
docker push $image

echo $image

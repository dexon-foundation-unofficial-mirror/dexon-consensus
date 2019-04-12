#!/bin/bash

IMAGE_TAG=asia.gcr.io/dexon-dev/dexcon-simulation:latest


build_binary() {
  make -C ../.. BUILD_IN_DOCKER=true
  cp -r ../../build .
}

build_docker_image() {
  docker build -t "${IMAGE_TAG}" .
  docker push "${IMAGE_TAG}"
}

start_simulation() {
  kubectl delete deployment dexcon-simulation --force --grace-period=0
  kubectl delete deployment dexcon-simulation-peer-server --force --grace-period=0
  sleep 10

  kubectl apply -f peer-server.yaml

  while true; do
    if kubectl get pods -l app=dexcon-simulation-peer-server | grep Running;
    then
      break
    fi
    sleep 1
  done

  kubectl apply -f node.yaml
}

main() {
  local num_nodes=$1

  if [ "$num_nodes" = "" ]; then
    num_nodes=7
  fi

  # Render configuration files.
  sed "s/{{numNodes}}/$num_nodes/" node.yaml.in > node.yaml
  sed "s/{{numNodes}}/$num_nodes/" config.toml.in > config.toml

  build_binary
  build_docker_image
  start_simulation
}

main "$@"

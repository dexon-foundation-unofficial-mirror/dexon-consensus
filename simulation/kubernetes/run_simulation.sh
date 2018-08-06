#!/bin/bash

IMAGE_TAG=asia.gcr.io/cobinhood/dexcon-simulation:latest


build_binary() {
  make DOCKER=true -C ../..
  cp -r ../../build .
}

build_docker_image() {
  docker build -t ${IMAGE_TAG} .
  docker push ${IMAGE_TAG}
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

  kubectl apply -f validator.yaml
}

main() {
  local num_validators=$1

  if [ "$num_validators" == "" ]; then
    num_validators=7
  fi

  # Render configuration files.
  sed "s/{{numValidators}}/$num_validators/" validator.yaml.in > validator.yaml
  sed "s/{{numValidators}}/$num_validators/" config.toml.in > config.toml

  build_binary
  build_docker_image
  start_simulation
}

main $*

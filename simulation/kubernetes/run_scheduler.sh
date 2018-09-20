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
  kubectl delete deployment dexcon-simulation-with-scheduler --force --grace-period=0
  sleep 30

  kubectl apply -f scheduler.yaml
}

main() {
    local num_nodes=$1
    local num_cpus=$2

    if [ "$num_nodes" == "" ]; then
      num_nodes=31
    fi

    if [ "$num_cpus" == "" ]; then
        num_cpus=2
    fi


    # Render configuration files.
    sed "s/{{numNodes}}/$num_nodes/" config.toml.in > config.toml
    sed "s/{{numCPUs}}/$num_cpus/" scheduler.yaml.in > scheduler.yaml

    build_binary
    build_docker_image
    start_simulation
}

main $*

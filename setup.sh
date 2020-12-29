# Run setup when in the directory. `setup_remote.sh` to initialize a remote server
apt-get update
# Why I don't dockerize this script?
# - It's good to have. Currently, the server spawns a docker container and putting this server in a container 
# install go and docker
#!/usr/bin/env bash

# install docker
sudo curl -fsSL https://get.docker.com -o get-docker.sh
sh get-docker.sh
sudo apt-get install -y golang-go
go build server.go
mkdir -p ./winvm/apps/
# shellcheck disable=SC2164
cd ./winvm
docker build . -t syncwine

# Start server by running
# - ./server
# open browser at addresss :8080

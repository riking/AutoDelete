#!/bin/bash

echo "Do not run this script directly; read it and copy out commands as appropriate."; cat $0; exit 0

# Check that go is installed

go version || echo "https://golang.org/dl/"

# Fetch the code

go get -u -v github.com/riking/AutoDelete/cmd/autodelete

# Create a data folder

FOLDER=$HOME/autodelete
mkdir -p "$FOLDER" ; cd "$FOLDER"

# Set up: fetch config.yml, build.sh, create a 'data' folder

cp "$(go env GOPATH)/src/github.com/riking/AutoDelete/docs/build.sh" .
cp "$(go env GOPATH)/src/github.com/riking/AutoDelete/config.example.yml" config.yml
mkdir data

# Create Discord bot account, fill in details
echo http://discordapp.com/developers/applications/me
editor config.yml

# Rebuild

./build.sh

# Run

./autodelete


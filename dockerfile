FROM golang:latest

RUN apk add --no-cache git && \
  go get -u -v github.com/riking/AutoDelete/cmd/autodelete

RUN mkdir -p /autodelete/data && \
  cp "/go/src/github.com/riking/AutoDelete/docs/build.sh" /autodelete/

EXPOSE 2202

WORKDIR /autodelete/

ENTRYPOINT ./build.sh && ./autodelete

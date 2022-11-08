FROM golang:1.18 as build

WORKDIR /go/src/app
COPY . .

ENV CGO_ENABLED=0
RUN go build -o /autodelete -ldflags="-s -w" -v github.com/riking/AutoDelete/cmd/autodelete

FROM gcr.io/distroless/static-debian11
COPY --from=build /autodelete /autodelete

ENV HOME=/
EXPOSE 2202
USER nonroot:nonroot

ENTRYPOINT [ "/autodelete"]

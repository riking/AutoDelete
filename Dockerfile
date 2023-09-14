FROM golang:1.21.1-alpine3.18 as builder 

WORKDIR /autodelete/

COPY . .

RUN mkdir -p output/ && go build -ldflags="-s -w" -v -o /autodelete/output/autodelete .

FROM alpine:3.18 as executer

WORKDIR /autodelete/

COPY --from=builder /autodelete/output/autodelete ./autodelete

RUN chmod +x ./autodelete

EXPOSE 2202

RUN ./autodelete

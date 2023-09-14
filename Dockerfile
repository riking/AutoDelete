FROM golang:1.21.1-alpine3.18 as base

FROM base as builder 

WORKDIR /autodelete/

COPY . .

RUN mkdir -p output/ && go build -ldflags="-s -w" -v -o /autodelete/output/autodelete .

FROM base as executer

WORKDIR /autodelete/

COPY --from=builder /autodelete/output/autodelete ./autodelete

RUN chmod +x ./autodelete

EXPOSE 2202

RUN ./autodelete

FROM golang:1.19 as build

WORKDIR /src
COPY . .

RUN CGO_ENABLED=0 go build --ldflags "-s -w" -o /usr/bin/run-connect main.go

FROM scratch as release
COPY --from=build /usr/bin/run-connect /

ENTRYPOINT ["/run-connect"]

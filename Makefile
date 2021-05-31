all:
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -o scp_quic_mac scp_quic.go
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o scp_quic_linux scp_quic.go
clean:
	rm -rf scp_mac
	rm -rf scp_linux

# scp_quic
scp with quic-go , pure pure, you can benefit at least double the bandwidth of tcp


server:

./scp_quic_linux -s -p /home/yuandan/quic-go/quic-go/example/scp/remote/ -a "192.168.1.1:4200"


client with file list:
scp_quic_mac -flist ./f.dat -d 1

client single file:
scp_quic_mac -f ./f.tar.gz -d 1

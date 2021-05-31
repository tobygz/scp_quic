# scp_quic
scp with quic-go , pure pure


server:

./scp_quic_linux -s -p /home/yuandan/quic-go/quic-go/example/scp/remote/


client with file list:
scp_quic_mac -flist ./f.dat -d 1

client single file:
scp_quic_mac -f ./f.tar.gz -d 1

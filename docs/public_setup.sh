exit 1

# ssh root@

apt install autossh sshfs tmux

# go https://golang.org/dl/
# prometheus https://prometheus.io/download/

rm -rf /usr/local/go && tar -C /usr/local -xzf go*.linux-amd64.tar.gz
tar xvf prometheus-*-linux-amd64.tar.gz
install prometheus-*.linux-amd64/prometheus /usr/local/bin/

adduser --disabled-password autodelete
install -oautodelete -gautodelete -d -m=0700 /home/autodelete/.ssh
install -oautodelete -gautodelete ~/.ssh/authorized_keys /home/autodelete/.ssh/authorized_keys

adduser --system prometheus
install -oprometheus -d -m=0755 /var/prometheus/data

ssh-keygen -t ed25519 -f /root/.ssh/id_hz_ed25519
cat /root/.ssh/id_hz_ed25519.pub

logout
# ssh autodelete@autodelete1

tee >> ~/.ssh/authorized_keys # paste the root key from above

logout
# ssh autodelete@

mkdir discord
mkdir -p go/src/github.com/riking/
cd go/src/github.com/riking; git clone https://github.com/riking/AutoDelete
cd ~/discord
REPO=$HOME/go/src/github.com/riking/AutoDelete
cp $REPO/docs/build.sh .
cp $REPO/docs/start_command.sh .

echo 'PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc

logout
# ssh root@

mkdir /mnt/autodelete
mkdir /mnt/autodelete/data
REPO=/home/autodelete/go/src/github.com/riking/AutoDelete
SVR_NR=4
install $REPO/docs/svr${SVR_NR}/mnt-autodelete-data.mount /etc/systemd/system
install $REPO/docs/svr${SVR_NR}/prometheus-autodelete-${SVR_NR}.service /etc/systemd/system
install $REPO/docs/svr${SVR_NR}/tunnel-${SVR_NR}-1.service /etc/systemd/system

# Accept host key
echo '|1|MVXohNgOqi8WQdZX5rh36DbtsHU=|k5RcJV+7cqe50lBYxwWMlBkQEV4= ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTYAAABBBCifg032LZWx+7ekZo+QKq5Nh0/bpIX1m20dQTgEEhHwJ07onLPIkMi5CZJp2PgpxfUr/cC7YTJc6vegtT0jy7g=' >> ~/.ssh/known_hosts
systemctl start mnt-autodelete-data.mount
systemctl start prometheus-autodelete-${SVR_NR}.service
systemctl start tunnel-${SVR_NR}-1.service

systemctl enable mnt-autodelete-data.mount prometheus-autodelete-${SVR_NR}.service tunnel-${SVR_NR}-1.service

logout
# ssh autodelete@

cd discord
ln -s /mnt/autodelete/data
<data/bans.yml | wc -l # verify sshfs works
nano config.yml

go version
./build.sh

REPO=/home/autodelete/go/src/github.com/riking/AutoDelete
cat $REPO/docs/start_command.sh
tmux new-session -s shard16

<Ctrl-B>, d

tmux a -t shard16

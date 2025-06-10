#!/bin/bash

set -e

echo "[*] Creating log-tester script..."
sudo mkdir -p /opt/log-tester
sudo tee /opt/log-tester/log-tester.sh > /dev/null <<'EOF'
#!/bin/bash
i=0
while true; do
  echo "systemd log line \$i"
  i=$((i+1))
  sleep 1
done
EOF
sudo chmod +x /opt/log-tester/log-tester.sh

echo "[*] Creating systemd unit..."
sudo tee /etc/systemd/system/log-tester.service > /dev/null <<EOF
[Unit]
Description=Systemd Log Tracker Test

[Service]
ExecStart=/opt/log-tester/log-tester.sh
StandardOutput=journal
Restart=always

[Install]
WantedBy=multi-user.target
EOF

echo "[*] Reloading systemd and starting service..."
sudo systemctl daemon-reexec
sudo systemctl daemon-reload
sudo systemctl enable --now log-tester.service

echo "[*] Tailing logs for log-tester.service..."
echo "    (Press Ctrl+C to stop watching logs)"
sleep 1
journalctl -u log-tester.service -f

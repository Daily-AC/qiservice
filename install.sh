#!/bin/bash

# LLM Service Station Installer for Ubuntu 24.04
# Usage: sudo ./install.sh

set -e

APP_NAME="qiservice"
INSTALL_DIR="/opt/$APP_NAME"
SERVICE_FILE="/etc/systemd/system/$APP_NAME.service"
USER="root" # Running as root for simplicity, or change to specific user

# Check for Root
if [ "$EUID" -ne 0 ]; then 
  echo "Please run as root (sudo ./install.sh)"
  exit 1
fi

echo "🚀 Installing $APP_NAME..."

# 1. Stop existing service if running
if systemctl is-active --quiet $APP_NAME; then
    echo "Stopping existing service..."
    systemctl stop $APP_NAME
fi

# 2. Prepare Directory
echo "📂 Creating install directory: $INSTALL_DIR"
mkdir -p $INSTALL_DIR

# 3. Build & Install Binary (Requires Go installed)
if ! command -v go &> /dev/null; then
    echo "❌ Go is not installed. Please install Go first (snap install go --classic)."
    exit 1
fi

echo "🔨 Building binary..."
go build -o "$INSTALL_DIR/$APP_NAME" cmd/server/main.go

# 4. Copy Assets
echo "📦 Copying web assets..."
# Clean up old web assets to avoid stale files
rm -rf "$INSTALL_DIR/web"
cp -r web "$INSTALL_DIR/"

# 5. Handle Config (Don't overwrite existing config)
if [ -f "$INSTALL_DIR/config.json" ]; then
    echo "⚠️  Existing config.json found. Skipping overwrite."
else
    echo "📝 Creating new config.json..."
    # Copy generic config or let app generate one.
    # We'll touch it so permissions can be set
    touch "$INSTALL_DIR/config.json"
fi

# 6. Set Permissions
chmod +x "$INSTALL_DIR/$APP_NAME"
# Optional: Change ownership if using a specific user
# chown -R $USER:$USER $INSTALL_DIR

# 7. Create Systemd Service
echo "⚙️  Creating systemd service..."
cat <<EOF > $SERVICE_FILE
[Unit]
Description=LLM Service Station
After=network.target

[Service]
Type=simple
User=$USER
WorkingDirectory=$INSTALL_DIR
ExecStart=$INSTALL_DIR/$APP_NAME
Restart=always
RestartSec=5
# Environment=GIN_MODE=release

[Install]
WantedBy=multi-user.target
EOF

# 8. Enable & Start
echo "🔌 Enabling and starting service..."
systemctl daemon-reload
systemctl enable $APP_NAME
systemctl start $APP_NAME

echo "✅ Installation Complete!"
echo "------------------------------------------------"
echo "🌐 Web Interface: http://YOUR_SERVER_IP:11451"
echo "------------------------------------------------"
echo "Management Commands:"
echo "  Start:    sudo systemctl start $APP_NAME"
echo "  Stop:     sudo systemctl stop $APP_NAME"
echo "  Restart:  sudo systemctl restart $APP_NAME"
echo "  Logs:     sudo journalctl -u $APP_NAME -f"
echo "------------------------------------------------"

# Check status
systemctl status $APP_NAME --no-pager

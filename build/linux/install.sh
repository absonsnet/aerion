#!/bin/bash

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
PURPLE='\033[0;35m'
NC='\033[0m' # No Color

echo -e "${PURPLE}
 ░▒▓██████▓▒░░▒▓████████▓▒░▒▓███████▓▒░░▒▓█▓▒░░▒▓██████▓▒░░▒▓███████▓▒░  
░▒▓█▓▒░░▒▓█▓▒░▒▓█▓▒░      ░▒▓█▓▒░░▒▓█▓▒░▒▓█▓▒░▒▓█▓▒░░▒▓█▓▒░▒▓█▓▒░░▒▓█▓▒░ 
░▒▓█▓▒░░▒▓█▓▒░▒▓█▓▒░      ░▒▓█▓▒░░▒▓█▓▒░▒▓█▓▒░▒▓█▓▒░░▒▓█▓▒░▒▓█▓▒░░▒▓█▓▒░ 
░▒▓████████▓▒░▒▓██████▓▒░ ░▒▓███████▓▒░░▒▓█▓▒░▒▓█▓▒░░▒▓█▓▒░▒▓█▓▒░░▒▓█▓▒░ 
░▒▓█▓▒░░▒▓█▓▒░▒▓█▓▒░      ░▒▓█▓▒░░▒▓█▓▒░▒▓█▓▒░▒▓█▓▒░░▒▓█▓▒░▒▓█▓▒░░▒▓█▓▒░ 
░▒▓█▓▒░░▒▓█▓▒░▒▓█▓▒░      ░▒▓█▓▒░░▒▓█▓▒░▒▓█▓▒░▒▓█▓▒░░▒▓█▓▒░▒▓█▓▒░░▒▓█▓▒░ 
░▒▓█▓▒░░▒▓█▓▒░▒▓████████▓▒░▒▓█▓▒░░▒▓█▓▒░▒▓█▓▒░░▒▓██████▓▒░░▒▓█▓▒░░▒▓█▓▒░ 
                                                                         
${NC}
"

# Print colored output
print_error() {
    echo -e "${RED}Error: $1${NC}" >&2
}

print_success() {
    echo -e "${GREEN}$1${NC}"
}

print_info() {
    echo -e "${YELLOW}$1${NC}"
}

# Check if running on Linux
if [[ "$(uname -s)" != "Linux" ]]; then
    print_error "This script is only for Linux systems"
    exit 1
fi

# Check if binary exists
if [[ ! -f "daak" ]]; then
    print_error "daak binary not found in current directory"
    echo "Please run this script from the directory containing the daak binary"
    exit 1
fi

# Check if desktop file exists
if [[ ! -f "net.absons.daak.desktop" ]]; then
    print_error "net.absons.daak.desktop file not found in current directory"
    echo "Please ensure the desktop file is in the same directory as this script"
    exit 1
fi

# Check if icons exist. New tarballs ship sized icons under icons/ plus a
# scalable SVG (#395); the root 256px PNG remains for backward compatibility.
if [[ ! -f "net.absons.daak.png" ]]; then
    print_error "net.absons.daak.png icon not found in current directory"
    echo "Please ensure the icon file is in the same directory as this script"
    exit 1
fi

# hicolor icon sizes (#395). Keep in sync with ICON_SIZES in the Makefile and
# .github/workflows/release.yml.
ICON_SIZES="32 48 64 128 256"

echo ""
print_info "Daak Email Client - Installation Script"
echo ""
echo "This script will install Daak on your system."
echo ""
echo "Choose installation type:"
echo "  1) System-wide (requires sudo, installs to /usr/local)"
echo "  2) User only (installs to ~/.local)"
echo ""

while true; do
    read -p "Enter your choice (1 or 2): " choice
    case $choice in
        1)
            INSTALL_TYPE="system"
            BIN_DIR="/usr/local/bin"
            APPS_DIR="/usr/share/applications"
            ICONS_BASE="/usr/share/icons/hicolor"
            NEEDS_SUDO=true
            break
            ;;
        2)
            INSTALL_TYPE="user"
            BIN_DIR="$HOME/.local/bin"
            APPS_DIR="$HOME/.local/share/applications"
            ICONS_BASE="$HOME/.local/share/icons/hicolor"
            NEEDS_SUDO=false
            break
            ;;
        *)
            print_error "Invalid choice. Please enter 1 or 2."
            ;;
    esac
done

echo ""
print_info "Installing Daak ($INSTALL_TYPE)..."
echo ""

# Function to run command with or without sudo
run_cmd() {
    if [[ "$NEEDS_SUDO" == true ]]; then
        sudo "$@"
    else
        "$@"
    fi
}

# Check for old desktop file and rename it (backwards compatibility)
OLD_DESKTOP_FILE="$APPS_DIR/aerion.desktop"
if [[ -f "$OLD_DESKTOP_FILE" ]]; then
    print_info "Found old aerion.desktop, renaming to aerion.desktop.backup..."
    run_cmd mv "$OLD_DESKTOP_FILE" "$APPS_DIR/aerion.desktop.backup"
    print_success "Old desktop file renamed to aerion.desktop.backup"
fi

# Check for pre-rename app-ID desktop file and rename it (backwards compatibility)
OLD_DESKTOP_FILE="$APPS_DIR/io.github.hkdb.Aerion.desktop"
if [[ -f "$OLD_DESKTOP_FILE" ]]; then
    print_info "Found old io.github.hkdb.Aerion.desktop, renaming to io.github.hkdb.Aerion.desktop.backup..."
    run_cmd mv "$OLD_DESKTOP_FILE" "$APPS_DIR/io.github.hkdb.Aerion.desktop.backup"
    print_success "Old desktop file renamed to io.github.hkdb.Aerion.desktop.backup"
fi

# Create directories if they don't exist (icon dirs are created per-size by
# install -D below)
print_info "Creating directories..."
run_cmd mkdir -p "$BIN_DIR"
run_cmd mkdir -p "$APPS_DIR"

# Install binary
print_info "Installing binary to $BIN_DIR..."
run_cmd install -Dm755 daak "$BIN_DIR/daak"

# Install desktop file
print_info "Installing desktop file to $APPS_DIR..."
run_cmd install -Dm644 net.absons.daak.desktop "$APPS_DIR/net.absons.daak.desktop"

# Install icons: all hicolor sizes when the tarball ships them (#395),
# falling back to the root 256px PNG for older tarballs
print_info "Installing icons to $ICONS_BASE..."
if [[ -d "icons" ]]; then
    for sz in $ICON_SIZES; do
        if [[ -f "icons/${sz}x${sz}/net.absons.daak.png" ]]; then
            run_cmd install -Dm644 "icons/${sz}x${sz}/net.absons.daak.png" \
                "$ICONS_BASE/${sz}x${sz}/apps/net.absons.daak.png"
        fi
    done
    if [[ -f "icons/scalable/net.absons.daak.svg" ]]; then
        run_cmd install -Dm644 "icons/scalable/net.absons.daak.svg" \
            "$ICONS_BASE/scalable/apps/net.absons.daak.svg"
    fi
else
    print_info "No sized icons in this package - installing 256px icon only"
    run_cmd install -Dm644 net.absons.daak.png "$ICONS_BASE/256x256/apps/net.absons.daak.png"
fi

# Update icon cache
print_info "Updating icon cache..."
if [[ "$INSTALL_TYPE" == "system" ]]; then
    run_cmd gtk-update-icon-cache -f -t /usr/share/icons/hicolor 2>/dev/null || true
else
    gtk-update-icon-cache -f -t "$HOME/.local/share/icons/hicolor" 2>/dev/null || true
fi

# Update desktop database
print_info "Updating desktop database..."
if [[ "$INSTALL_TYPE" == "system" ]]; then
    run_cmd update-desktop-database /usr/share/applications 2>/dev/null || true
else
    update-desktop-database "$HOME/.local/share/applications" 2>/dev/null || true
fi

echo ""
print_success "✓ Installation complete!"
echo ""

# Additional setup instructions
if [[ "$INSTALL_TYPE" == "user" ]]; then
    if [[ ":$PATH:" != *":$HOME/.local/bin:"* ]]; then
        print_info "Note: $HOME/.local/bin is not in your PATH"
        echo "You may need to add it to your PATH by adding this line to your ~/.bashrc or ~/.zshrc:"
        echo "  export PATH=\"\$HOME/.local/bin:\$PATH\""
        echo ""
    fi
fi

echo "You may need to log out and back in for the application to appear in your menu."
echo ""
echo "To set Daak as your default email client, run:"
echo "  xdg-mime default net.absons.daak.desktop x-scheme-handler/mailto"
echo ""
echo "To start Daak, run:"
echo "  daak --dbus-notify"
echo ""

# New API Electron Desktop App

This directory contains the Electron wrapper for ModelPort, based on New API and providing a native desktop application with system tray support for Windows, macOS, and Linux.

## Prerequisites

### 1. Go Binary (Required)
The Electron app requires the compiled Go binary to function. You have two options:

**Option A: Use existing binary (without Go installed)**
```bash
# If you have a pre-built binary (e.g., new-api-macos)
cp ../new-api-macos ../new-api
```

**Option B: Build from source (requires Go)**
TODO

### 2. Electron Dependencies
```bash
cd electron
npm install
```

## Development

Start the backend, the frontend, and Electron in separate terminals:
```bash
# Repository root
go run main.go

# Repository root
make dev-web

# electron/
npm run dev-app
```

This will:
- Use the Go backend on port 3000
- Use the Rsbuild frontend development server on port 5173
- Open an Electron window with DevTools enabled
- Create a system tray icon (menu bar on macOS)
- Connect to the separately started Go backend; when started with `make dev-api`, its default SQLite file is `modelport.db` in the repository root

## Building for Production

### Quick Build
```bash
# From electron/, build the frontend, Go binary, and desktop package
./build.sh

# Or package an existing binary for the current platform
npm run build

# Platform-specific builds
npm run build:mac    # Creates .dmg and .zip
npm run build:win    # Creates .exe installer
npm run build:linux  # Creates .AppImage and .deb
```

### Build Output
- Built applications are in `electron/dist/`
- macOS: `.dmg` (installer) and `.zip` (portable)
- Windows: `.exe` (installer) and portable exe
- Linux: `.AppImage` and `.deb`

## Configuration

### Port
Default port is 3000. To change, edit `main.js`:
```javascript
const PORT = 3000; // Change to desired port
```

### Database Location
- **Development**: the Go backend owns the database path. Set `SQLITE_PATH` explicitly when starting it; `make dev-api` defaults to `modelport.db` in the repository root.
- **Production**:
  - macOS: `~/Library/Application Support/ModelPort/data/modelport.db`
  - Windows: `%APPDATA%/ModelPort/data/modelport.db`
  - Linux: `~/.config/ModelPort/data/modelport.db`

The packaged desktop app creates the production directory on first launch. It uses a new SQLite file and does not migrate an old New API database; start with a new data directory for a fresh deployment.

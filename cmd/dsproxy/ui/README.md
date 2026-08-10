# DSProxy UI

Modern web UI for DSProxy built with React, PatternFly, and Vite.

## Development

### Prerequisites

- Node.js 18+ and npm

### Install Dependencies

```bash
npm install
```

### Run Development Server

```bash
npm run dev
```

This will start the Vite development server on `http://localhost:5173`.

### Build for Production

```bash
npm run build
```

This will create a production build in the `dist/` directory, which is embedded into the dsproxy binary.

## Integration

The UI is embedded into the dsproxy binary using Go's `embed` directive. The production build must be created before building the dsproxy binary:

```bash
# From the project root
make build-dsproxy
```

This will:
1. Install npm dependencies
2. Build the production UI bundle
3. Compile dsproxy with the embedded UI assets

The UI is served on port 3001 by default (configurable with `--ui-port` or `DSPROXY_UI_PORT`). It binds to `127.0.0.1` only — access it via port-forward or behind an authenticated proxy.

> **Security note:** the policy API (`/api/policy`) requires a valid DSProxy JWT (`Authorization: Bearer <token>`), the same token the proxy endpoints accept. Without a token the API returns `401 Unauthorized`. When accessing the UI directly, inject the token into browser requests via a port-forward/proxy setup or a browser extension; do not expose port 3001 on the network.

## Architecture

- **Framework**: React 18
- **UI Library**: PatternFly 5
- **Build Tool**: Vite 5
- **Language**: TypeScript
- **Routing**: React Router (when needed)

## Structure

```
ui/
├── src/
│   ├── App.tsx          # Main application component
│   ├── main.tsx         # Application entry point
│   └── vite-env.d.ts    # TypeScript declarations
├── dist/                # Production build (generated, embedded in Go binary)
├── index.html           # HTML template
├── package.json         # Dependencies and scripts
├── tsconfig.json        # TypeScript configuration
└── vite.config.ts       # Vite configuration
```

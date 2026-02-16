# APIPod Electron Desktop App - Full Implementation Plan

## Objective

Build a standalone Electron desktop app ("APIPod") that serves as a proxy injector and management client for Claude Code. The app patches Claude Code's configuration to route API traffic through the APIPod smart proxy, and provides a full UI for authentication, proxy connection, subscription management, usage analytics, and request logs.

## Token Estimate

~150K tokens across multiple sessions (large multi-phase project)

---

## System Architecture

### High-Level Data Flow

```
+-------------------+          +--------------------+         +------------------+
|   Claude Code     |  reads   |  ~/.claude/        |  set by |  APIPod Electron |
|   (CLI Tool)      |--------->|  settings.json     |<--------|  (Desktop App)   |
+-------------------+          +--------------------+         +------------------+
        |                                                           |
        | ANTHROPIC_BASE_URL                                        | REST API
        | ANTHROPIC_API_KEY                                         | (Sanctum)
        v                                                           v
+-------------------+                                    +--------------------+
| APIPod Smart      |                                    | APIPod Dashboard   |
| Proxy (Go)        |                                    | (Laravel)          |
| Port 8081         |                                    | Port 8000          |
+-------------------+                                    +--------------------+
        |                                                           |
        | Routes to providers                                       | Reads usage logs
        v                                                           | from shared DB
+-------------------+                                               |
| Upstream LLM APIs |<----------------------------------------------+
| (Anthropic,       |
|  OpenAI, Groq)    |
+-------------------+
```

### How the Injection Works

1. Claude Code reads `~/.claude/settings.json` on startup
2. If `env.ANTHROPIC_BASE_URL` is set, Claude Code uses that URL instead of `api.anthropic.com`
3. If `env.ANTHROPIC_API_KEY` is set, Claude Code uses that key for authentication
4. APIPod Electron writes these values to point Claude Code at the smart proxy
5. The smart proxy validates the API key, routes to upstream providers, and logs usage

### Two-Token System

| Token | Purpose | Where Stored | Who Validates |
|-------|---------|-------------|---------------|
| Sanctum Token | Authenticates Electron app to Dashboard API | electron-store (encrypted via safeStorage) | Laravel Dashboard |
| User apitoken | Authenticates Claude Code to Smart Proxy | Written into ~/.claude/settings.json | Go Smart Proxy |

The login endpoint returns BOTH tokens. They are completely separate credentials.

---

## Tech Stack

| Layer | Technology | Purpose |
|-------|-----------|---------|
| Desktop Framework | Electron 33+ | Cross-platform desktop app |
| Build Tool | Vite 6 | Fast bundling for renderer |
| Language | TypeScript | Type safety across all layers |
| UI Framework | React 19 | Component-based UI |
| Styling | Tailwind CSS 4 | Utility-first dark theme |
| State Management | Zustand | Lightweight global state |
| Data Fetching | @tanstack/react-query | Caching, auto-refresh, pagination |
| Charts | Recharts | Usage analytics visualization |
| HTTP Client | Axios | API calls with interceptors |
| Local Storage | electron-store | Persistent settings |
| Secure Storage | Electron safeStorage | Encrypted credential storage |
| Router | react-router-dom v7 | Client-side page navigation |

---

## Project Structure

```
apipod-electron/
|
+-- package.json                    # Dependencies and scripts
+-- electron-builder.yml            # Build/distribution config
+-- tsconfig.json                   # TypeScript config (renderer)
+-- tsconfig.node.json              # TypeScript config (main/preload)
+-- vite.config.ts                  # Vite bundler config
+-- tailwind.config.js              # Tailwind dark theme config
+-- postcss.config.js               # PostCSS for Tailwind
+-- index.html                      # HTML entry point for renderer
|
+-- resources/
|   +-- icon.icns                   # macOS app icon
|   +-- icon.ico                    # Windows app icon
|   +-- icon.png                    # Linux app icon
|   +-- tray-icon.png               # System tray icon (16x16)
|   +-- tray-icon-active.png        # Tray icon when proxy enabled
|
+-- src/
|   |
|   +-- main/                       # ===== ELECTRON MAIN PROCESS =====
|   |   +-- main.ts                 # App entry: window, tray, lifecycle
|   |   +-- tray.ts                 # System tray icon and context menu
|   |   +-- ipc-handlers.ts         # All IPC handler registrations
|   |   +-- config-patcher.ts       # Read/write ~/.claude/settings.json
|   |   +-- auto-launch.ts          # Start app on login
|   |   +-- secure-store.ts         # Encrypted credential storage
|   |
|   +-- preload/                    # ===== PRELOAD (BRIDGE) =====
|   |   +-- preload.ts              # contextBridge exposing IPC to renderer
|   |
|   +-- renderer/                   # ===== REACT APP (RENDERER) =====
|       +-- main.tsx                # React entry point
|       +-- App.tsx                 # Root component with router + layout
|       +-- index.css               # Tailwind imports + global styles
|       |
|       +-- types/
|       |   +-- electron.d.ts       # Type declarations for window.electronAPI
|       |   +-- api.d.ts            # API response type definitions
|       |
|       +-- api/                    # API layer
|       |   +-- client.ts           # Axios instance with interceptors
|       |   +-- auth.ts             # login(), logout(), getProfile()
|       |   +-- usage.ts            # summary(), byModel(), daily(), hourly()
|       |   +-- subscriptions.ts    # list(), getCurrent()
|       |   +-- logs.ts             # getLogs() with pagination
|       |
|       +-- store/                  # Zustand stores
|       |   +-- auth-store.ts       # User state, login/logout actions
|       |   +-- settings-store.ts   # Proxy URL, auto-launch, preferences
|       |   +-- usage-store.ts      # Cached analytics data
|       |
|       +-- hooks/                  # Custom React hooks
|       |   +-- useProxyStatus.ts   # Poll proxy enabled/disabled state
|       |   +-- useApi.ts           # Generic query wrapper
|       |
|       +-- lib/                    # Utility functions
|       |   +-- utils.ts            # Format costs, tokens, dates
|       |   +-- constants.ts        # Default URLs, refresh intervals
|       |
|       +-- pages/                  # Page components (one per route)
|       |   +-- LoginPage.tsx
|       |   +-- DashboardPage.tsx
|       |   +-- ConnectionPage.tsx
|       |   +-- SubscriptionsPage.tsx
|       |   +-- LogsPage.tsx
|       |   +-- SettingsPage.tsx
|       |
|       +-- components/             # Reusable UI components
|           |
|           +-- layout/
|           |   +-- Sidebar.tsx         # Left nav with links + proxy status
|           |   +-- Header.tsx          # Top bar with user info + connection dot
|           |   +-- AppShell.tsx        # Layout wrapper (sidebar + header + content)
|           |
|           +-- auth/
|           |   +-- LoginForm.tsx       # Email/password form
|           |   +-- AuthGuard.tsx       # Redirect to login if not authenticated
|           |
|           +-- dashboard/
|           |   +-- SummaryCards.tsx     # Cards: total cost, tokens, requests
|           |   +-- DailyChart.tsx      # Line chart: usage over 30 days
|           |   +-- ModelBreakdown.tsx   # Donut chart: cost by model
|           |   +-- TopModels.tsx       # Ranked list of top models
|           |
|           +-- connection/
|           |   +-- ProxyToggle.tsx     # Big connect/disconnect button
|           |   +-- ConnectionStatus.tsx # Green/red dot with label
|           |   +-- ProxySettings.tsx   # Proxy URL input field
|           |
|           +-- subscriptions/
|           |   +-- SubscriptionList.tsx # Cards for each available plan
|           |   +-- CurrentPlan.tsx      # User's active plan with quota
|           |
|           +-- logs/
|           |   +-- LogsTable.tsx       # Paginated table of requests
|           |   +-- LogFilters.tsx      # Date, model, status filters
|           |
|           +-- ui/                     # Generic UI primitives
|               +-- Button.tsx
|               +-- Card.tsx
|               +-- Input.tsx
|               +-- Badge.tsx
|               +-- Spinner.tsx
|               +-- DateRangePicker.tsx
```

---

## Files Affected in Existing Projects

### apipod-dashboard (Laravel) - New Files

| File | Action | Purpose |
|------|--------|---------|
| `app/Http/Controllers/Api/AuthController.php` | CREATE | Login/logout endpoints |
| `app/Http/Controllers/Api/UserController.php` | CREATE | User profile endpoint |
| `app/Http/Controllers/Api/SubscriptionController.php` | CREATE | List subscriptions |
| `app/Http/Controllers/Api/LogController.php` | CREATE | Paginated usage logs |
| `routes/api.php` | MODIFY | Add new route definitions |
| `config/cors.php` | MODIFY | Allow Electron app origin |

### apipod-dashboard (Laravel) - Reference Files (read-only)

| File | Why We Need It |
|------|---------------|
| `app/Http/Controllers/UsageAnalyticsController.php` | Pattern for response format |
| `app/Models/User.php` | User model structure, relationships |
| `app/Models/Subscription.php` | Subscription model, quotaItems relationship |
| `app/Models/UsageLog.php` | Log model, fields available |
| `app/Services/TokenUsageService.php` | Analytics calculation logic |

---

## Detailed Phase Breakdown

---

### Phase 1: Project Scaffolding

**Goal**: Working Electron + React + Tailwind app with hot-reload

#### Step 1.1: Initialize Project

```bash
mkdir -p /Users/rpay/Documents/project/apipod-electron
cd /Users/rpay/Documents/project/apipod-electron
npm init -y
```

Update `package.json`:
- name: "apipod-electron"
- main: "dist/main/main.js"
- scripts: dev, build, preview, package

#### Step 1.2: Install Dependencies

**Production dependencies:**
```
react react-dom react-router-dom
zustand @tanstack/react-query
axios recharts
electron-store
```

**Dev dependencies:**
```
electron electron-builder
vite @vitejs/plugin-react
typescript @types/react @types/react-dom @types/node
tailwindcss postcss autoprefixer
concurrently wait-on
```

#### Step 1.3: Configure TypeScript

Create `tsconfig.json` for renderer (target: ES2020, jsx: react-jsx)
Create `tsconfig.node.json` for main process (target: ES2020, module: commonjs)

#### Step 1.4: Configure Vite

```typescript
// vite.config.ts
export default defineConfig({
  plugins: [react()],
  base: './',            // Relative paths for Electron file:// protocol
  build: {
    outDir: 'dist/renderer',
  },
});
```

#### Step 1.5: Configure Tailwind

```javascript
// tailwind.config.js
module.exports = {
  content: ['./index.html', './src/renderer/**/*.{ts,tsx}'],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        // Custom dark theme palette
        surface: { DEFAULT: '#0f172a', light: '#1e293b', lighter: '#334155' },
        accent: { DEFAULT: '#6366f1', hover: '#818cf8' },
      }
    }
  }
};
```

#### Step 1.6: Create Entry Files

- `index.html` - Basic HTML shell loading renderer bundle
- `src/renderer/main.tsx` - ReactDOM.createRoot
- `src/renderer/App.tsx` - "Hello APIPod" placeholder
- `src/renderer/index.css` - `@tailwind base; @tailwind components; @tailwind utilities;`
- `src/main/main.ts` - Basic BrowserWindow loading index.html

#### Step 1.7: Verify Dev Workflow

```bash
npm run dev  # Should open Electron window with React app, hot-reload working
```

**Deliverable**: Empty Electron window with dark background and "Hello APIPod" text.

---

### Phase 2: Main Process Core

**Goal**: Config patching, IPC bridge, system tray, secure storage all working

#### Step 2.1: Config Patcher (`src/main/config-patcher.ts`)

**Functions:**
```typescript
readClaudeSettings(): ClaudeSettings
  // Read ~/.claude/settings.json
  // Return {} if file doesn't exist or is malformed
  // Preserve ALL existing keys

writeClaudeSettings(settings: ClaudeSettings): void
  // Atomic write: write to temp file, then rename
  // Create ~/.claude/ directory if needed
  // Pretty-print JSON with 2-space indent

enableProxy(proxyUrl: string, apiToken: string): ClaudeSettings
  // Read current settings
  // Add/update env.ANTHROPIC_BASE_URL and env.ANTHROPIC_API_KEY
  // Preserve any other env variables
  // Write back and return new settings

disableProxy(): ClaudeSettings
  // Read current settings
  // Delete env.ANTHROPIC_BASE_URL and env.ANTHROPIC_API_KEY
  // Remove env key entirely if empty
  // Write back and return new settings

getProxyStatus(): { enabled: boolean; proxyUrl?: string; apiToken?: string }
  // Read settings and check if both env keys exist
```

**Edge cases to handle:**
- `~/.claude/` directory doesn't exist -> create with `mkdir -p`
- `settings.json` doesn't exist -> start from `{}`
- `settings.json` is empty string -> start from `{}`
- `settings.json` has invalid JSON -> start from `{}`
- `settings.json` has existing `env` with other variables -> preserve them
- File permissions -> use same permissions as original (default 644)

#### Step 2.2: Secure Store (`src/main/secure-store.ts`)

```typescript
import { safeStorage } from 'electron';
import Store from 'electron-store';

const store = new Store({ name: 'apipod-config' });

// Save Sanctum token (encrypted)
saveSanctumToken(token: string): void
  // Encrypt with safeStorage.encryptString()
  // Store base64 encoded in electron-store

getSanctumToken(): string | null
  // Read from store, decrypt with safeStorage.decryptString()

clearSanctumToken(): void
  // Delete from store

// Save non-sensitive settings (unencrypted)
saveProxyUrl(url: string): void
getProxyUrl(): string  // default: 'http://127.0.0.1:8081'
saveAutoLaunch(enabled: boolean): void
getAutoLaunch(): boolean
saveDashboardUrl(url: string): void
getDashboardUrl(): string  // default: 'http://127.0.0.1:8000'
```

#### Step 2.3: IPC Handlers (`src/main/ipc-handlers.ts`)

Register all handlers with `ipcMain.handle()`:

```typescript
// Proxy config
'proxy:get-status'     -> configPatcher.getProxyStatus()
'proxy:enable'         -> configPatcher.enableProxy(proxyUrl, apiToken)
'proxy:disable'        -> configPatcher.disableProxy()

// Auth storage
'auth:save-token'      -> secureStore.saveSanctumToken(token)
'auth:get-token'       -> secureStore.getSanctumToken()
'auth:clear-token'     -> secureStore.clearSanctumToken()

// Settings
'settings:get'         -> { proxyUrl, dashboardUrl, autoLaunch }
'settings:set-proxy-url'    -> secureStore.saveProxyUrl(url)
'settings:set-dashboard-url' -> secureStore.saveDashboardUrl(url)
'settings:set-auto-launch'  -> secureStore.saveAutoLaunch(enabled)
                                + app.setLoginItemSettings(...)

// App control
'app:quit'             -> app.isQuitting = true; app.quit()
'app:minimize-to-tray' -> mainWindow.hide()
```

#### Step 2.4: Preload Script (`src/preload/preload.ts`)

```typescript
contextBridge.exposeInMainWorld('electronAPI', {
  proxy: {
    getStatus:  () => ipcRenderer.invoke('proxy:get-status'),
    enable:     (url, token) => ipcRenderer.invoke('proxy:enable', url, token),
    disable:    () => ipcRenderer.invoke('proxy:disable'),
  },
  auth: {
    saveToken:  (token) => ipcRenderer.invoke('auth:save-token', token),
    getToken:   () => ipcRenderer.invoke('auth:get-token'),
    clearToken: () => ipcRenderer.invoke('auth:clear-token'),
  },
  settings: {
    get:             () => ipcRenderer.invoke('settings:get'),
    setProxyUrl:     (url) => ipcRenderer.invoke('settings:set-proxy-url', url),
    setDashboardUrl: (url) => ipcRenderer.invoke('settings:set-dashboard-url', url),
    setAutoLaunch:   (v) => ipcRenderer.invoke('settings:set-auto-launch', v),
  },
  app: {
    quit:            () => ipcRenderer.invoke('app:quit'),
    minimizeToTray:  () => ipcRenderer.invoke('app:minimize-to-tray'),
  },
});
```

#### Step 2.5: Type Declarations (`src/renderer/types/electron.d.ts`)

Full TypeScript interface for `window.electronAPI` matching the preload script.

#### Step 2.6: System Tray (`src/main/tray.ts`)

```typescript
createTray(mainWindow: BrowserWindow): Tray
  // Create tray with icon (16x16 template image for macOS)
  // Context menu:
  //   "APIPod" (label, disabled)
  //   ---
  //   "Status: Connected" / "Status: Disconnected"
  //   "Toggle Proxy" -> enable/disable proxy
  //   ---
  //   "Open Dashboard" -> mainWindow.show()
  //   ---
  //   "Quit" -> app.quit()

updateTrayStatus(tray: Tray, connected: boolean): void
  // Update icon (green/gray)
  // Update menu label
```

#### Step 2.7: Main Window Lifecycle (`src/main/main.ts`)

```typescript
// Window config
{
  width: 1100, height: 750,
  minWidth: 900, minHeight: 600,
  titleBarStyle: 'hiddenInset',  // macOS native look
  backgroundColor: '#0f172a',
  show: false,  // Show after ready-to-show
}

// Close -> hide to tray (unless quitting)
mainWindow.on('close', (e) => {
  if (!app.isQuitting) {
    e.preventDefault();
    mainWindow.hide();
  }
});

// Single instance lock
const gotLock = app.requestSingleInstanceLock();
if (!gotLock) app.quit();
app.on('second-instance', () => mainWindow.show());
```

#### Step 2.8: Auto Launch (`src/main/auto-launch.ts`)

```typescript
setAutoLaunch(enabled: boolean): void
  app.setLoginItemSettings({
    openAtLogin: enabled,
    openAsHidden: true,  // Start minimized to tray
  });

getAutoLaunch(): boolean
  return app.getLoginItemSettings().openAtLogin;
```

**Deliverable**: Electron app with system tray, working config patcher (verified by manually checking settings.json), and IPC bridge ready.

---

### Phase 3: Laravel API Endpoints

**Goal**: All API endpoints needed by the Electron app are available and tested

#### Step 3.1: Auth Controller (`app/Http/Controllers/Api/AuthController.php`)

```php
class AuthController extends Controller
{
    // POST /api/auth/login
    public function login(Request $request)
    {
        // Validate: email (required, email), password (required)
        // Find user by email
        // Verify password with Hash::check
        // If invalid -> 401 { success: false, message: "Invalid credentials" }
        // Check user.active == true, not expired
        // Create Sanctum personal access token
        // Return:
        {
            "success": true,
            "data": {
                "token": "sanctum-token-string",
                "user": {
                    "id": 1,
                    "name": "John",
                    "email": "john@example.com",
                    "apitoken": "sk-proxy-token",  // <-- The proxy API key
                    "active": true,
                    "expires_at": "2026-12-31T00:00:00Z",
                    "subscription": {
                        "sub_id": "sub_001",
                        "sub_name": "Pro Plan",
                        "price": 50000
                    }
                }
            }
        }
    }

    // POST /api/auth/logout
    public function logout(Request $request)
    {
        // Delete current token: $request->user()->currentAccessToken()->delete()
        // Return { success: true }
    }
}
```

#### Step 3.2: User Controller (`app/Http/Controllers/Api/UserController.php`)

```php
class UserController extends Controller
{
    // GET /api/user/profile
    public function profile(Request $request)
    {
        // Get authenticated user
        // Eager load: subscription.quotaItems.llmModel
        // Return:
        {
            "success": true,
            "data": {
                "id": 1,
                "name": "John",
                "email": "john@example.com",
                "apitoken": "sk-proxy-token",
                "active": true,
                "expires_at": "2026-12-31T00:00:00Z",
                "subscription": {
                    "sub_id": "sub_001",
                    "sub_name": "Pro Plan",
                    "price": 50000,
                    "quota_items": [
                        {
                            "quota_id": "q_001",
                            "model_name": "claude-sonnet-4-5",
                            "token_limit": 1000000,
                            "percentage_weight": 50
                        }
                    ]
                }
            }
        }
    }
}
```

#### Step 3.3: Subscription Controller (`app/Http/Controllers/Api/SubscriptionController.php`)

```php
class SubscriptionController extends Controller
{
    // GET /api/subscriptions
    public function index()
    {
        // Get all subscriptions with quotaItems.llmModel
        // Return:
        {
            "success": true,
            "data": [
                {
                    "sub_id": "sub_001",
                    "sub_name": "Free",
                    "price": 0,
                    "quota_items": [...]
                },
                {
                    "sub_id": "sub_002",
                    "sub_name": "Pro",
                    "price": 50000,
                    "quota_items": [...]
                }
            ]
        }
    }
}
```

#### Step 3.4: Log Controller (`app/Http/Controllers/Api/LogController.php`)

```php
class LogController extends Controller
{
    // GET /api/logs
    public function index(Request $request)
    {
        // Query UsageLog for authenticated user
        // Order by timestamp DESC
        // Filters (all optional):
        //   - model: filter by routed_model
        //   - status: filter by status code
        //   - start_date: timestamp >= value
        //   - end_date: timestamp <= value
        // Paginate with per_page param (default 25)
        // Return Laravel's default paginated response:
        {
            "success": true,
            "data": {
                "data": [
                    {
                        "usage_id": "log_001",
                        "routed_model": "claude-sonnet-4-5",
                        "input_tokens": 1500,
                        "output_tokens": 800,
                        "status": 200,
                        "timestamp": "2026-02-16T10:30:00Z"
                    }
                ],
                "current_page": 1,
                "last_page": 10,
                "per_page": 25,
                "total": 243
            }
        }
    }
}
```

#### Step 3.5: Update Routes (`routes/api.php`)

```php
use App\Http\Controllers\Api\AuthController;
use App\Http\Controllers\Api\UserController;
use App\Http\Controllers\Api\SubscriptionController;
use App\Http\Controllers\Api\LogController;

// Public routes
Route::post('/auth/login', [AuthController::class, 'login']);

// Protected routes
Route::middleware(['auth:sanctum'])->group(function () {
    // Auth
    Route::post('/auth/logout', [AuthController::class, 'logout']);

    // User
    Route::get('/user/profile', [UserController::class, 'profile']);

    // Subscriptions
    Route::get('/subscriptions', [SubscriptionController::class, 'index']);

    // Logs
    Route::get('/logs', [LogController::class, 'index']);

    // Existing usage endpoints (already defined)
    // GET /usage/summary
    // GET /usage/by-model
    // GET /usage/daily
    // GET /usage/hourly
    // GET /usage/top-models
});
```

#### Step 3.6: CORS Configuration

Update `config/cors.php` to allow requests from Electron:
```php
'allowed_origins' => ['http://localhost:*', 'file://*', 'app://*'],
```

#### Step 3.7: Test Endpoints

Test all endpoints with curl:
```bash
# Login
curl -X POST http://localhost:8000/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"test@example.com","password":"password"}'

# Profile (with token from login)
curl http://localhost:8000/api/user/profile \
  -H "Authorization: Bearer {sanctum-token}"

# Subscriptions
curl http://localhost:8000/api/subscriptions \
  -H "Authorization: Bearer {sanctum-token}"

# Logs
curl "http://localhost:8000/api/logs?per_page=10" \
  -H "Authorization: Bearer {sanctum-token}"
```

**Deliverable**: All 5 new API endpoints working and returning correct JSON.

---

### Phase 4: Authentication Flow

**Goal**: User can login, token is stored securely, protected routes work

#### Step 4.1: API Client (`src/renderer/api/client.ts`)

```typescript
// Create Axios instance
const apiClient = axios.create({
  headers: { 'Accept': 'application/json', 'Content-Type': 'application/json' },
});

// Request interceptor: attach Sanctum token
apiClient.interceptors.request.use(async (config) => {
  // Get dashboard URL from electron settings
  const settings = await window.electronAPI.settings.get();
  config.baseURL = `${settings.dashboardUrl}/api`;

  // Get stored Sanctum token
  const token = await window.electronAPI.auth.getToken();
  if (token) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  return config;
});

// Response interceptor: handle 401
apiClient.interceptors.response.use(
  (res) => res,
  async (err) => {
    if (err.response?.status === 401) {
      await window.electronAPI.auth.clearToken();
      window.location.hash = '#/login';
    }
    return Promise.reject(err);
  }
);
```

#### Step 4.2: Auth API Functions (`src/renderer/api/auth.ts`)

```typescript
login(email: string, password: string): Promise<LoginResponse>
  // POST /auth/login
  // Returns { token, user }

logout(): Promise<void>
  // POST /auth/logout

getProfile(): Promise<UserProfile>
  // GET /user/profile
```

#### Step 4.3: Auth Store (`src/renderer/store/auth-store.ts`)

```typescript
interface AuthState {
  user: UserProfile | null;
  isAuthenticated: boolean;
  isLoading: boolean;

  login(email: string, password: string): Promise<void>;
    // 1. Call API login
    // 2. Store Sanctum token via IPC (encrypted)
    // 3. Set user state
    // 4. Auto-enable proxy with user.apitoken

  logout(): Promise<void>;
    // 1. Call API logout
    // 2. Clear Sanctum token via IPC
    // 3. Disable proxy (remove from settings.json)
    // 4. Clear user state

  checkAuth(): Promise<void>;
    // 1. Check if Sanctum token exists in store
    // 2. If yes, fetch profile from API
    // 3. If API fails (401), clear token
    // 4. Set isAuthenticated accordingly

  refreshProfile(): Promise<void>;
    // Re-fetch profile from API
}
```

#### Step 4.4: Login Page (`src/renderer/pages/LoginPage.tsx`)

```
+--------------------------------------------------+
|                                                    |
|              [APIPod Logo]                          |
|                                                    |
|         +----------------------------+             |
|         |  Server URL                |             |
|         |  [http://localhost:8000  ]  |             |
|         |                            |             |
|         |  Email                     |             |
|         |  [user@example.com      ]  |             |
|         |                            |             |
|         |  Password                  |             |
|         |  [**************        ]  |             |
|         |                            |             |
|         |  [     Sign In          ]  |             |
|         |                            |             |
|         |  Error: Invalid creds      |             |
|         +----------------------------+             |
|                                                    |
+--------------------------------------------------+
```

Features:
- Server URL field (defaults to stored dashboard URL)
- Email + password fields
- Loading state on submit
- Error message display
- Remember server URL in electron-store

#### Step 4.5: Auth Guard (`src/renderer/components/auth/AuthGuard.tsx`)

```typescript
// Wraps all protected routes
// On mount: calls checkAuth()
// If not authenticated: redirect to /login
// If loading: show spinner
// If authenticated: render children
```

#### Step 4.6: App Router (`src/renderer/App.tsx`)

```typescript
<HashRouter>
  <Routes>
    <Route path="/login" element={<LoginPage />} />
    <Route element={<AuthGuard><AppShell /></AuthGuard>}>
      <Route path="/" element={<DashboardPage />} />
      <Route path="/connection" element={<ConnectionPage />} />
      <Route path="/subscriptions" element={<SubscriptionsPage />} />
      <Route path="/logs" element={<LogsPage />} />
      <Route path="/settings" element={<SettingsPage />} />
    </Route>
  </Routes>
</HashRouter>
```

Note: Using `HashRouter` because Electron loads from `file://` protocol.

**Deliverable**: Login flow works end-to-end. Token stored securely. Protected routes redirect to login.

---

### Phase 5: Connection/Proxy UI

**Goal**: One-click proxy enable/disable with visual feedback

#### Step 5.1: Connection Page Layout

```
+--------------------------------------------------+
| Connection                                         |
+--------------------------------------------------+
|                                                    |
|  +----------------------------------------------+ |
|  | Proxy Status                                  | |
|  |                                               | |
|  |  [====GREEN DOT====]  Connected               | |
|  |                                               | |
|  |  Proxy URL: https://proxy.apipod.io/v1       | |
|  |  API Key:   sk-82cc...705a9                   | |
|  |                                               | |
|  |  [ Disconnect ]                               | |
|  +----------------------------------------------+ |
|                                                    |
|  +----------------------------------------------+ |
|  | Configuration                                 | |
|  |                                               | |
|  |  Smart Proxy URL                              | |
|  |  [http://127.0.0.1:8081/v1              ]     | |
|  |                                               | |
|  |  Claude Settings File                         | |
|  |  ~/.claude/settings.json                      | |
|  |  Last modified: 2026-02-16 10:30:00           | |
|  |                                               | |
|  |  [ Open Settings File ]                       | |
|  +----------------------------------------------+ |
|                                                    |
|  +----------------------------------------------+ |
|  | Health Check                                  | |
|  |  Smart Proxy: [GREEN] Online                  | |
|  |  Dashboard API: [GREEN] Online                | |
|  |  Last checked: 5 seconds ago                  | |
|  +----------------------------------------------+ |
+--------------------------------------------------+
```

#### Step 5.2: ProxyToggle Component

```typescript
// States:
// - disconnected (red, "Connect" button)
// - connecting (yellow, spinner)
// - connected (green, "Disconnect" button)
// - error (red, error message + "Retry" button)

// Connect flow:
// 1. Get proxy URL from settings
// 2. Get user.apitoken from auth store
// 3. Call window.electronAPI.proxy.enable(proxyUrl, apitoken)
// 4. Verify by reading status back
// 5. Update tray icon via IPC

// Disconnect flow:
// 1. Call window.electronAPI.proxy.disable()
// 2. Verify by reading status back
// 3. Update tray icon
```

#### Step 5.3: Health Check

```typescript
// Poll every 30 seconds:
// 1. GET {proxyUrl}/health -> check if smart proxy is up
// 2. GET {dashboardUrl}/api/usage/summary -> check if dashboard is up
// Display green/red indicators
```

#### Step 5.4: Tray Status Sync

When proxy status changes (enable/disable), the renderer notifies the main process to update the tray icon and menu. This happens automatically through the IPC handlers - when `proxy:enable` or `proxy:disable` is called, the handler also calls `updateTrayStatus()`.

**Deliverable**: Toggle proxy on/off from UI and tray. Verified by checking `~/.claude/settings.json` contents.

---

### Phase 6: Dashboard Analytics

**Goal**: Beautiful usage analytics with charts

#### Step 6.1: API Functions (`src/renderer/api/usage.ts`)

```typescript
getSummary(startDate?, endDate?): Promise<UsageSummary>
  // GET /usage/summary

getByModel(startDate?, endDate?): Promise<ModelUsage[]>
  // GET /usage/by-model

getDaily(startDate?, endDate?): Promise<DailyUsage[]>
  // GET /usage/daily

getHourly(date?): Promise<HourlyUsage[]>
  // GET /usage/hourly

getTopModels(limit?): Promise<ModelUsage[]>
  // GET /usage/top-models
```

#### Step 6.2: Dashboard Page Layout

```
+--------------------------------------------------+
| Dashboard                            [Last 30d v] |
+--------------------------------------------------+
|                                                    |
| +------------+ +------------+ +------------+       |
| | Total Cost | | Tokens     | | Requests   |       |
| | $12.34     | | 1.2M       | | 458        |       |
| | +5.2% ^    | | +12% ^     | | +8% ^      |       |
| +------------+ +------------+ +------------+       |
|                                                    |
| +----------------------------------------------+  |
| | Daily Usage                                   |  |
| |                                               |  |
| |  $2.0 |        *                              |  |
| |  $1.5 |     *     *  *                        |  |
| |  $1.0 |  *           *  *     *               |  |
| |  $0.5 | *                 *  *   *  *          |  |
| |       +---+---+---+---+---+---+---+---+       |  |
| |       Jan 18  Jan 22  Jan 26  Jan 30          |  |
| +----------------------------------------------+  |
|                                                    |
| +---------------------+ +----------------------+  |
| | Model Breakdown     | | Top Models           |  |
| |                     | |                       |  |
| |     [PIE CHART]     | | 1. claude-sonnet $8  |  |
| |  claude-sonnet 65%  | | 2. gpt-4o       $3  |  |
| |  gpt-4o        25%  | | 3. groq-llama   $1  |  |
| |  groq-llama    10%  | |                       |  |
| +---------------------+ +----------------------+  |
+--------------------------------------------------+
```

#### Step 6.3: Summary Cards (`SummaryCards.tsx`)

- Use `@tanstack/react-query` with `useQuery('usage-summary', getSummary)`
- 3 cards in a row: Total Cost, Total Tokens, Request Count
- Format cost as USD with 2 decimals
- Format tokens with K/M suffix
- Auto-refresh every 60 seconds

#### Step 6.4: Daily Chart (`DailyChart.tsx`)

- Recharts `<LineChart>` or `<AreaChart>`
- X-axis: dates, Y-axis: cost in USD
- Tooltip showing date + cost + tokens
- Responsive container
- Dark theme colors (indigo line, slate grid)

#### Step 6.5: Model Breakdown (`ModelBreakdown.tsx`)

- Recharts `<PieChart>` with `<Pie>`
- Each slice = one model, sized by cost
- Legend with model names and percentages
- Custom colors for each model

#### Step 6.6: Top Models (`TopModels.tsx`)

- Simple ranked list
- Each row: rank, model name, cost, token count
- Color-coded bar showing relative usage

**Deliverable**: Dashboard with live data from API, auto-refreshing charts.

---

### Phase 7: Subscriptions & Logs

**Goal**: View available plans and browse request logs

#### Step 7.1: Subscriptions Page

```
+--------------------------------------------------+
| Subscriptions                                      |
+--------------------------------------------------+
|                                                    |
| Current Plan: Pro Plan                             |
| Expires: December 31, 2026                         |
|                                                    |
| +----------------------------------------------+  |
| | Your Quota                                    |  |
| |                                               |  |
| | claude-sonnet-4-5   [====------] 400K/1M     |  |
| | gpt-4o              [==--------] 200K/1M     |  |
| | groq-llama-3        [=---------] 100K/1M     |  |
| +----------------------------------------------+  |
|                                                    |
| Available Plans                                    |
|                                                    |
| +-------------+ +-------------+ +-------------+   |
| | Free        | | Pro         | | Enterprise  |   |
| | $0/mo       | | $50K/mo     | | $200K/mo    |   |
| |             | |             | |             |   |
| | 100K tokens | | 1M tokens   | | 10M tokens  |   |
| | 2 models    | | All models  | | All models  |   |
| |             | |             | | Priority    |   |
| | [Current]   | | [Upgrade]   | | [Contact]   |   |
| +-------------+ +-------------+ +-------------+   |
+--------------------------------------------------+
```

#### Step 7.2: Logs Page

```
+--------------------------------------------------+
| Logs                                               |
+--------------------------------------------------+
| [Model: All v] [Status: All v] [Date: Last 7d v] |
+--------------------------------------------------+
| Timestamp          | Model          | In   | Out  | Status |
|--------------------|----------------|------|------|--------|
| 2026-02-16 10:30   | claude-sonnet  | 1.5K | 800  | 200    |
| 2026-02-16 10:28   | claude-sonnet  | 2.1K | 1.2K | 200    |
| 2026-02-16 10:25   | gpt-4o         | 500  | 300  | 200    |
| 2026-02-16 10:20   | claude-sonnet  | 3.0K | 1.8K | 500    |
| ...                |                |      |      |        |
+--------------------------------------------------+
| < 1 2 3 ... 10 >   Showing 1-25 of 243           |
+--------------------------------------------------+
```

#### Step 7.3: Log Filters (`LogFilters.tsx`)

- Model dropdown: populated from available models
- Status dropdown: 200, 400, 401, 429, 500, etc.
- Date range picker: preset options (Today, Last 7d, Last 30d, Custom)
- Filters update URL query params for shareable state

#### Step 7.4: Logs Table (`LogsTable.tsx`)

- Paginated table with 25 rows per page
- Columns: Timestamp, Model, Input Tokens, Output Tokens, Status
- Status badge colors: green (2xx), yellow (4xx), red (5xx)
- Click row to expand details (if available)
- Page navigation at bottom

**Deliverable**: Subscription plans displayed, logs table with pagination and filtering.

---

### Phase 8: Polish & UX

**Goal**: Production-quality UX

#### Step 8.1: App Shell Layout (`AppShell.tsx`)

```
+--------+------------------------------------------+
|        |  Header: User name | Connection dot       |
| Side   +------------------------------------------+
| bar    |                                           |
|        |  Page content area                        |
| [icon] |                                           |
| Dash   |                                           |
| [icon] |                                           |
| Conn   |                                           |
| [icon] |                                           |
| Subs   |                                           |
| [icon] |                                           |
| Logs   |                                           |
| [icon] |                                           |
| Sett   |                                           |
|        |                                           |
|        |                                           |
| [dot]  |                                           |
| Status |                                           |
+--------+------------------------------------------+
```

- Sidebar: 60px wide with icons, expand on hover to show labels
- Header: user avatar/name on right, connection status dot
- Active page highlighted in sidebar

#### Step 8.2: Settings Page

```
+--------------------------------------------------+
| Settings                                           |
+--------------------------------------------------+
|                                                    |
| Server Configuration                               |
|   Dashboard URL: [http://localhost:8000        ]   |
|   Proxy URL:     [http://127.0.0.1:8081/v1    ]   |
|                                                    |
| Application                                        |
|   [x] Start on login                              |
|   [x] Minimize to tray on close                   |
|   [ ] Show notifications                          |
|                                                    |
| Account                                            |
|   Logged in as: john@example.com                   |
|   [ Sign Out ]                                     |
|                                                    |
| About                                              |
|   APIPod v1.0.0                                    |
|   Electron v33.0.0                                 |
+--------------------------------------------------+
```

#### Step 8.3: Loading & Error States

- Every page: skeleton loading state while data fetches
- Error boundary: "Something went wrong" with retry button
- Empty state: "No data yet" with helpful message
- Toast notifications for actions (proxy enabled, proxy disabled, error)

#### Step 8.4: Window Behavior

- Close button -> minimize to tray (not quit)
- macOS: hide dock icon when minimized to tray
- Double-click tray icon -> show window
- Single instance enforcement (second launch shows existing window)

#### Step 8.5: Notifications

- System notification when proxy is enabled/disabled
- Optional: notification when usage exceeds threshold

**Deliverable**: Polished, production-ready UI with consistent dark theme.

---

### Phase 9: Build & Distribution

**Goal**: Distributable macOS app

#### Step 9.1: electron-builder Configuration

```yaml
# electron-builder.yml
appId: com.apipod.electron
productName: APIPod
directories:
  output: release
mac:
  category: public.app-category.developer-tools
  target:
    - dmg
    - zip
  icon: resources/icon.icns
dmg:
  contents:
    - x: 130
      y: 220
    - x: 410
      y: 220
      type: link
      path: /Applications
```

#### Step 9.2: Build Script

```json
{
  "scripts": {
    "dev": "concurrently \"vite\" \"wait-on tcp:5173 && electron .\"",
    "build": "tsc && vite build && electron-builder",
    "build:mac": "tsc && vite build && electron-builder --mac"
  }
}
```

#### Step 9.3: Test Production Build

```bash
npm run build
# Verify .dmg created in release/ directory
# Install and test all features
```

**Deliverable**: `.dmg` installer for macOS.

---

## Risks & Considerations

1. **Claude Code settings format changes**: The config patcher must be defensive. Always parse with try/catch, preserve unknown keys, never overwrite the entire file.

2. **Two-token confusion**: Login returns Sanctum token (for API) + user apitoken (for proxy). UI should clearly distinguish these. Never expose Sanctum token to user.

3. **CORS**: Electron renderer makes requests to Laravel. Must configure Laravel CORS to allow `http://localhost:*`. Alternative: route API calls through main process via IPC (more secure but more complex).

4. **Proxy URL**: Must be configurable (localhost for dev, domain for prod). Default stored in electron-store.

5. **Concurrent config writes**: Only one Electron instance runs (single-instance lock), but Claude Code could also modify settings.json. Use atomic writes (temp file + rename).

6. **Sanctum token expiry**: If token expires, API calls will 401. Interceptor redirects to login page.

## Status
- [ ] Not started

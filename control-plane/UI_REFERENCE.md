# UI Visual Reference

This document provides a text-based visual reference for all pages in the web UI.

## Layout Structure

All pages (except login/register) follow this structure:

```
┌─────────────────────────────────────────────────────────────┐
│ ⚡ Processor | Dashboard | Jobs | API Keys | Webhooks | Admin │ user@email.com | Logout
├─────────────────────────────────────────────────────────────┤
│ [Flash Messages - Success/Error/Info/Warning]                │
├─────────────────────────────────────────────────────────────┤
│                                                               │
│  [Page Content]                                               │
│                                                               │
└─────────────────────────────────────────────────────────────┘
```

## Pages

### 1. Login Page (`/login`)

```
┌──────────────────────────────────┐
│                                  │
│         Sign In                  │
│  Welcome back to Processor       │
│  Control Plane                   │
│                                  │
│  Email                           │
│  ┌─────────────────────────────┐│
│  │ email@example.com           ││
│  └─────────────────────────────┘│
│                                  │
│  Password                        │
│  ┌─────────────────────────────┐│
│  │ ••••••••                    ││
│  └─────────────────────────────┘│
│                                  │
│  ┌─────────────────────────────┐│
│  │      Sign In               ││
│  └─────────────────────────────┘│
│                                  │
│  Don't have an account? Sign up  │
│                                  │
└──────────────────────────────────┘
```

### 2. Register Page (`/register`)

```
┌──────────────────────────────────┐
│                                  │
│      Create Account              │
│  Start using Processor           │
│  Control Plane                   │
│                                  │
│  Email *                         │
│  ┌─────────────────────────────┐│
│  │                             ││
│  └─────────────────────────────┘│
│                                  │
│  Password *                      │
│  ┌─────────────────────────────┐│
│  │                             ││
│  └─────────────────────────────┘│
│  Minimum 8 characters            │
│                                  │
│  Confirm Password *              │
│  ┌─────────────────────────────┐│
│  │                             ││
│  └─────────────────────────────┘│
│                                  │
│  ┌─────────────────────────────┐│
│  │   Create Account           ││
│  └─────────────────────────────┘│
│                                  │
│  Already have an account? Sign in│
│                                  │
└──────────────────────────────────┘
```

### 3. Dashboard (`/`)

```
Dashboard
Welcome back, user@example.com

┌─────────┐ ┌─────────┐ ┌─────────┐ ┌─────────┐
│ Total   │ │ Queued  │ │ Running │ │Success  │
│  42     │ │   3     │ │   1     │ │  38     │
└─────────┘ └─────────┘ └─────────┘ └─────────┘

┌───────────────────────────────────────────────┐
│ Recent Jobs              [New Job]            │
├───────────────────────────────────────────────┤
│ ID          Type      Status    Created       │
│ job_abc123  noop     Succeeded  2 hours ago   │
│ job_def456  sleep    Running    1 hour ago    │
│ job_ghi789  echo     Queued     30 mins ago   │
└───────────────────────────────────────────────┘
                [View All Jobs]

┌──────────────────┐  ┌──────────────────┐
│ Quick Actions    │  │ Account Info     │
├──────────────────┤  ├──────────────────┤
│ Create New Job   │  │ Plan: free       │
│ Manage API Keys  │  │ Member Since:    │
│ Configure Webhooks│  │ Jan 15, 2026     │
│ Account Settings │  │                  │
└──────────────────┘  └──────────────────┘
```

### 4. Jobs List (`/jobs`)

```
Jobs
Manage and monitor your jobs            [New Job]

┌─────────────────────────────────────────────┐
│ Filter: [All Statuses ▼]                   │
└─────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────┐
│ ID          Type     Status    Priority  Attempts  Created   │
├──────────────────────────────────────────────────────────────┤
│ job_abc123  noop    [Succeeded]   0       1/3    2 hours ago │ [View]
│ job_def456  sleep   [Running]     5       1/3    1 hour ago  │ [View]
│ job_ghi789  echo    [Queued]      0       0/3    30 mins ago │ [View]
│ job_jkl012  ollama  [Failed]     -10      2/3    1 day ago   │ [View]
└──────────────────────────────────────────────────────────────┘

Showing 1 to 25 of 100 jobs         [Previous] [Next]
```

### 5. Create Job (`/jobs/new`)

```
Create New Job
Submit a new job to the processing queue

┌─────────────────────────────────────────────┐
│ Job Type *                                  │
│ ┌─────────────────────────────────────────┐│
│ │ Select a job type... ▼                  ││
│ └─────────────────────────────────────────┘│
│                                             │
│ Input Metadata (JSON)                       │
│ ┌─────────────────────────────────────────┐│
│ │ {"key": "value"}                        ││
│ │                                         ││
│ │                                         ││
│ └─────────────────────────────────────────┘│
│ Optional JSON metadata to pass to the job   │
│                                             │
│ Priority Weight                             │
│ ┌─────────────────────────────────────────┐│
│ │ 0                                       ││
│ └─────────────────────────────────────────┘│
│ Higher values = higher priority (-100 to 100)│
│                                             │
│ Max Attempts                                │
│ ┌─────────────────────────────────────────┐│
│ │ 3                                       ││
│ └─────────────────────────────────────────┘│
│ Maximum retry attempts on failure (1-10)    │
│                                             │
│ Notifications                               │
│ ☐ Send email notification on completion    │
│ ☐ Send webhook notification on completion  │
│                                             │
│ [Create Job]  [Cancel]                      │
└─────────────────────────────────────────────┘
```

### 6. Job Details (`/jobs/:id`)

```
Job Details                              [Back to Jobs]
job_abc1234567890

┌──────────────────┐  ┌──────────────────┐
│ Overview         │  │ Timeline         │
├──────────────────┤  ├──────────────────┤
│ Status           │  │ Created          │
│ [Succeeded]      │  │ Jan 15 3:00 PM   │
│                  │  │                  │
│ Job Type         │  │ Queued           │
│ echo_metadata    │  │ Jan 15 3:00 PM   │
│                  │  │                  │
│ Priority Weight  │  │ Started          │
│ 0                │  │ Jan 15 3:01 PM   │
│                  │  │                  │
│ Attempts         │  │ Completed        │
│ 1 / 3            │  │ Jan 15 3:01 PM   │
└──────────────────┘  └──────────────────┘

┌─────────────────────────────────────────────┐
│ Input Metadata                              │
├─────────────────────────────────────────────┤
│ {                                           │
│   "message": "Hello, world!",               │
│   "timestamp": "2026-01-15T15:00:00Z"       │
│ }                                           │
└─────────────────────────────────────────────┘

┌─────────────────────────────────────────────┐
│ Result Metadata                             │
├─────────────────────────────────────────────┤
│ {                                           │
│   "echoed": {                               │
│     "message": "Hello, world!",             │
│     "timestamp": "2026-01-15T15:00:00Z"     │
│   },                                        │
│   "processed_at": "2026-01-15T15:01:00Z"    │
│ }                                           │
└─────────────────────────────────────────────┘

┌─────────────────────────────────────────────┐
│ Execution Attempts                          │
├─────────────────────────────────────────────┤
│ #  Worker      Started   Finished   Status  │
│ 1  wrk_xyz123  3:01 PM   3:01 PM  [Succeeded]
└─────────────────────────────────────────────┘
```

### 7. API Keys (`/api-keys`)

```
API Keys                              [Create API Key]
Manage your API authentication keys

┌─────────────────────────────────────────────────────────────┐
│ Name              Key Prefix       Created        Status    │
├─────────────────────────────────────────────────────────────┤
│ Production       pk_abc123def...   Jan 15, 2026  [Active]   │ [Revoke]
│ Development      pk_xyz789ghi...   Jan 10, 2026  [Active]   │ [Revoke]
│ Old Key          pk_old123old...   Jan 1, 2026   [Revoked]  │
└─────────────────────────────────────────────────────────────┘

When creating a new key, you'll see:

┌─────────────────────────────────────────────────────────────┐
│ ✓ API Key Created Successfully                              │
├─────────────────────────────────────────────────────────────┤
│ Save this key now - you won't be able to see it again!      │
│                                                              │
│ pk_abc123def456_7890ghijklmnopqrstuvwxyz1234567890         │
└─────────────────────────────────────────────────────────────┘
```

### 8. Webhooks (`/webhooks`)

```
Webhooks                              [Create Webhook]
Configure webhook notifications for job events

┌──────────────────────────────────────────────────────────────────┐
│ Target URL                 Events            Status     Created  │
├──────────────────────────────────────────────────────────────────┤
│ https://example.com/hook  job.succeeded    [Active]  Jan 15, 2026│ [Delete]
│                           job.failed                              │
│                           job.dead_letter                         │
│                                                                   │
│ https://test.com/webhook  job.succeeded  [Inactive]  Jan 10, 2026│ [Delete]
└──────────────────────────────────────────────────────────────────┘
```

### 9. Profile (`/profile`)

```
Profile & Settings
Manage your account settings

┌─────────────────────────────────────────────┐
│ Account Information                         │
├─────────────────────────────────────────────┤
│ Email                                       │
│ user@example.com                            │
│                                             │
│ User ID                                     │
│ usr_abc1234567890                           │
│                                             │
│ Plan Tier                                   │
│ Free                                        │
│                                             │
│ Member Since                                │
│ Jan 15, 2026                                │
│                                             │
│ Role                                        │
│ [Administrator]                             │
└─────────────────────────────────────────────┘

┌─────────────────────────────────────────────┐
│ Admin Access                                │
├─────────────────────────────────────────────┤
│ You have administrator privileges.          │
│ Access admin features:                      │
│                                             │
│ [Admin Dashboard]                           │
│ [All Jobs]                                  │
│ [Workers]                                   │
│ [Users]                                     │
└─────────────────────────────────────────────┘

┌─────────────────────────────────────────────┐
│ Actions                                     │
├─────────────────────────────────────────────┤
│ [Sign Out]                                  │
└─────────────────────────────────────────────┘
```

### 10. Admin Dashboard (`/admin`)

```
Admin Dashboard                      [Back to Dashboard]
System overview and statistics

┌────────┐ ┌────────┐ ┌────────┐ ┌────────┐
│ Total  │ │ Total  │ │ Active │ │ Jobs   │
│ Users  │ │ Jobs   │ │Workers │ │ Today  │
│   25   │ │  1,234 │ │    5   │ │   87   │
└────────┘ └────────┘ └────────┘ └────────┘

┌──────────────────┐  ┌──────────────────┐
│ Job Status       │  │ Quick Links      │
│ Breakdown        │  │                  │
├──────────────────┤  ├──────────────────┤
│ Queued      12   │  │ View All Jobs    │
│ Running      5   │  │ Manage Workers   │
│ Succeeded  987   │  │ Manage Users     │
│ Failed      45   │  │ Back to Dashboard│
│ Dead Letter  3   │  │                  │
└──────────────────┘  └──────────────────┘
```

### 11. Admin Jobs (`/admin/jobs`)

```
All Jobs (Admin)                        [Back to Admin]
View and manage all system jobs

┌─────────────────────────────────────────────┐
│ Filter: [All Statuses ▼]                   │
└─────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────┐
│ ID          User      Type    Status   Worker     Created   │
├─────────────────────────────────────────────────────────────┤
│ job_abc123  usr_xyz  noop   [Queued]  -          2 hrs ago  │ [View]
│ job_def456  usr_123  sleep  [Running] wrk_abc    1 hr ago   │ [View]
│ job_ghi789  usr_xyz  echo   [Success] wrk_def    30 min ago │ [View]
└─────────────────────────────────────────────────────────────┘

Showing 1 to 50 of 1,234 jobs       [Previous] [Next]
```

### 12. Admin Workers (`/admin/workers`)

```
Workers                                 [Back to Admin]
Monitor data-plane workers

┌────────────────────────────────────────────────────────────────┐
│ ID          Name      Status  Version  Capabilities  Current   │
│                                                       Job       │
├────────────────────────────────────────────────────────────────┤
│ wrk_abc123  worker-1 [Active]  1.0.0   noop         job_xyz123│
│                                         sleep                  │
│                                         echo                   │
│                                         ollama                 │
│                                                                │
│                                         Last Seen: 1 min ago   │
│                                                                │
│ wrk_def456  worker-2 [Active]  1.0.0   noop          -        │
│                                         sleep                  │
│                                                                │
│                                         Last Seen: 30 sec ago  │
└────────────────────────────────────────────────────────────────┘
```

### 13. Admin Users (`/admin/users`)

```
Users                                   [Back to Admin]
Manage user accounts

┌────────────────────────────────────────────────────────────────┐
│ ID         Email             Plan   Role    Status    Created  │
├────────────────────────────────────────────────────────────────┤
│ usr_abc123 admin@example.com free  [Admin] [Active]  Jan 1    │ 42
│ usr_def456 user@example.com  free  [User]  [Active]  Jan 15   │ 12
│ usr_ghi789 test@example.com  free  [User]  [Disabled] Jan 10  │ 0
└────────────────────────────────────────────────────────────────┘
```

## Status Badges

Status badges are color-coded:

- `[Queued]` - Blue background
- `[Leased]` - Yellow/orange background
- `[Running]` - Yellow/orange background
- `[Succeeded]` - Green background
- `[Failed]` - Red background
- `[Dead Letter]` - Dark red background
- `[Canceled]` - Gray background
- `[Active]` - Green background
- `[Inactive]` - Gray background
- `[Revoked]` - Gray background

## Flash Messages

Flash messages appear at the top of the page:

**Success (Green)**
```
┌──────────────────────────────────────────────┐
│ ✓ Job created successfully            [×]   │
└──────────────────────────────────────────────┘
```

**Error (Red)**
```
┌──────────────────────────────────────────────┐
│ ✗ Failed to create job                [×]   │
└──────────────────────────────────────────────┘
```

**Info (Blue)**
```
┌──────────────────────────────────────────────┐
│ ℹ Processing your request...          [×]   │
└──────────────────────────────────────────────┘
```

**Warning (Yellow)**
```
┌──────────────────────────────────────────────┐
│ ⚠ This action cannot be undone        [×]   │
└──────────────────────────────────────────────┘
```

## Empty States

When lists are empty, friendly messages are shown:

```
┌─────────────────────────────────────────────┐
│                                             │
│                    📋                        │
│                                             │
│            No jobs yet                      │
│                                             │
│     Create your first job to get started    │
│                                             │
│            [Create Job]                     │
│                                             │
└─────────────────────────────────────────────┘
```

Similar empty states exist for:
- No API keys: 🔑
- No webhooks: 🔔
- No workers: 🤖
- No users: 👥

## Responsive Design

On mobile devices, the layout adapts:
- Navigation becomes collapsible
- Tables scroll horizontally
- Grid layouts stack vertically
- Forms take full width
- Buttons become full-width

## Color Palette

- **Primary**: Blue (#2563eb)
- **Success**: Green (#16a34a)
- **Warning**: Orange (#ea580c)
- **Danger**: Red (#dc2626)
- **Info**: Cyan (#0891b2)
- **Background**: White/Light Gray
- **Text**: Dark Gray/Black
- **Borders**: Light Gray (#e5e7eb)

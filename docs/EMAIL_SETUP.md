# Email Notifications Setup

Email notifications are configured via environment variables stored in a `.env` file (gitignored for security).

## Quick Setup

### 1. For Gmail (Recommended for Testing)

1. **Enable 2-Factor Authentication** on your Google account
2. **Create an App Password**:
   - Go to https://myaccount.google.com/apppasswords
   - Generate a new app password for "Mail"
   - Copy the 16-character password

3. **Configure `.env` file**:

If you don't have a `.env` file yet:
```bash
cp .env.example .env
```

Then edit `.env` and update the SMTP settings:

```bash
SMTP_HOST=smtp.gmail.com
SMTP_PORT=587
SMTP_USER=your-email@gmail.com
SMTP_PASS=your-16-char-app-password
SMTP_FROM=your-email@gmail.com
```

**Important:** Never commit `.env` to git - it contains secrets and is already gitignored.

4. **Restart**:
```bash
make dev-down
make dev-up
```

### 2. For Other SMTP Providers

Update your `.env` file with provider-specific settings:

**SendGrid:**
```bash
SMTP_HOST=smtp.sendgrid.net
SMTP_PORT=587
SMTP_USER=apikey
SMTP_PASS=your-sendgrid-api-key
SMTP_FROM=noreply@yourdomain.com
```

**Mailgun:**
```bash
SMTP_HOST=smtp.mailgun.org
SMTP_PORT=587
SMTP_USER=postmaster@yourdomain.mailgun.org
SMTP_PASS=your-mailgun-password
SMTP_FROM=noreply@yourdomain.com
```

**AWS SES:**
```yaml
- SMTP_HOST=email-smtp.us-east-1.amazonaws.com
- SMTP_PORT=587
- SMTP_USER=your-ses-smtp-username
- SMTP_PASS=your-ses-smtp-password
- SMTP_FROM=verified-email@yourdomain.com
```

**Office 365:**
```yaml
- SMTP_HOST=smtp.office365.com
- SMTP_PORT=587
- SMTP_USER=your-email@yourdomain.com
- SMTP_PASS=your-password
- SMTP_FROM=your-email@yourdomain.com
```

## Testing

1. **Check logs** to see if emails are being sent:
```bash
docker-compose logs -f control-plane | grep EMAIL
```

2. **Create a test job** with email notifications enabled:
   - Go to http://localhost:8080/jobs/new
   - Check "Send email notification on completion"
   - Submit the job
   - Check your inbox

## Current Status

Without SMTP configuration, you'll see emails in logs like:
```
[EMAIL] To: admin@example.com, Subject: Job Completed Successfully
Your job job_abc123 (echo_metadata) has completed successfully.

Processed 1 email notifications
```

With SMTP configured, emails will actually be sent to the recipient's inbox.

## Troubleshooting

**Emails not sending?**

1. Check logs for errors:
   ```bash
   docker-compose logs control-plane | grep -i "email\|smtp"
   ```

2. Verify SMTP credentials are correct

3. Check firewall/port blocking (port 587 or 465)

4. For Gmail: Ensure app password is used (not regular password)

5. Restart after configuration changes:
   ```bash
   docker-compose restart control-plane
   ```

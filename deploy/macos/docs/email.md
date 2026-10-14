# Password-reset email

Read this if you want players to reset forgotten passwords by email. It is optional:
without `SMTP_HOST` the reset link is only written to `/var/log/cribbager/app.log`.

## Any SMTP provider

Set these in `/usr/local/etc/cribbager/env`, then `sudo launchctl kickstart -k system/org.cribbager.app`:

| Key | Value |
| --- | --- |
| `SMTP_HOST`, `SMTP_PORT` | Provider's server and a STARTTLS port, usually 587 (implicit-TLS port 465 is not supported) |
| `SMTP_USER`, `SMTP_PASS` | Provider's credentials |
| `SMTP_FROM` | An address on a domain the provider has verified for you. `Cribbager <noreply@example.com>` is fine; the server sends the bare address in the SMTP envelope. |
| `BASE_URL` | `https://<your-domain>`, so the emailed link points at your site |

Links are single-use and expire after one hour.

## Worked example: Resend

1. Create an account at https://resend.com, then **Domains > Add Domain** with `example.com`
   or a sending subdomain such as `mail.example.com`.
2. Add the SPF `TXT`, DKIM and MX records Resend lists to your DNS. In Cloudflare set each
   to **DNS only** (grey cloud). Click **Verify**; it can take minutes to an hour.
   Check with `dig +short TXT resend._domainkey.example.com`.
3. **API Keys > Create API Key**, permission **Sending access**, limited to that domain.
   The key (`re_...`) is shown once.
4. In the env file:

   ```
   SMTP_HOST=smtp.resend.com
   SMTP_PORT=587
   SMTP_USER=resend
   SMTP_PASS=re_...
   SMTP_FROM=Cribbager <noreply@example.com>
   ```

Free tier: 3,000 emails/month and 100/day at last check. Sending pauses at the limit
instead of billing you. Plans change, including how many domains the free tier allows;
check Resend's pricing page. Reset volume for a small site is tiny.

## Test

On the home page choose **Forgot password?** and enter a registered account's email. The
message should arrive within a minute.

## Troubleshooting

```bash
grep -i -E 'smtp|email|reset' /var/log/cribbager/app.log
```

| Symptom | Cause |
| --- | --- |
| `501` or a bad-sender error | `SMTP_FROM` is not on a verified domain, or is malformed |
| Domain shows not verified | A DNS record is missing, or is still proxied (orange cloud) |
| Nothing arrives, no error | Check spam; check the provider's logs; confirm the app was restarted |
| Link opens the wrong site | `BASE_URL` is wrong |
| Reset link only in the log | `SMTP_HOST` is empty |

# Bounce processing

Dispatchly records the bounces that mail providers report, and acts on them
with the rules under `bounce.actions`.

## Webhooks

Enable **Bounce webhooks** in Settings, then point the provider at
`/webhooks/bounce/<service>`.

| Service  | Endpoint                          | Authentication        |
|----------|-----------------------------------|-----------------------|
| Sendgrid | `/webhooks/bounce/sendgrid`       | the key in Settings   |
| Postmark | `/webhooks/bounce/postmark`       | basic auth            |

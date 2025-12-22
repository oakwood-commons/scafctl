# Email Notifier Example

This example demonstrates how to send templated emails to multiple recipients via an external API. It showcases template rendering, foreach iteration with complex objects, API provider integration, and resolver validation.

## Overview

The solution sends personalized emails to a list of recipients by:
1. Resolving recipient list and email metadata from CLI or defaults
2. Rendering email template with recipient-specific context
3. Iterating over recipients and calling SendGrid API for each

## Key Concepts Demonstrated

### Resolvers with Complex Data

- **Array of objects**: `recipients` resolver accepts array of objects `[{"name":"...","email":"..."}]`
- **CLI input**: Complex JSON passed via `-r recipients='[...]'`
- **Type validation**: CEL expressions validate each recipient has required fields
- **String templates**: `messageBody` and `senderName` support multi-line content

### Actions with Foreach and API Provider

- **Foreach iteration**: Action iterates over each recipient in the array
- **Item binding**: `foreach: {over: _.recipients, as: recipient}` makes `_.recipient` available for current item
- **API payload**: Constructs SendGrid API payload with personalized recipient
- **Inline HTML**: Email body inlined in action (no separate template needed for API payloads)
- **Outputs**: Returns count of emails sent and last recipient processed

### Conditional Execution

- **Guard clause**: `when: _.recipients != null && _.recipients.size() > 0` prevents empty sends

## Usage Examples

### Default recipients and subject

```bash
scafctl run solution:email-notifier
```

Uses built-in defaults: Alice and Bob with subject "Hello from scafctl"

### Custom single recipient

```bash
scafctl run solution:email-notifier \
  -r recipients='[{"name":"Carol","email":"carol@example.com"}]' \
  -r subject="Welcome Aboard"
```

### Multiple custom recipients with custom message

```bash
scafctl run solution:email-notifier \
  -r recipients='[
    {"name":"Dave","email":"dave@company.com"},
    {"name":"Eve","email":"eve@company.com"}
  ]' \
  -r subject="Team Announcement" \
  -r messageBody="Important update from the team." \
  -r senderEmail="team@company.com" \
  -r senderName="Team Lead"
```

### Preview email payloads without sending

```bash
scafctl run solution:email-notifier \
  -r recipients='[{"name":"Test","email":"test@example.com"}]' \
  --dry-run
```

### Load recipients from file

```bash
# recipients.json contains array of recipient objects
scafctl run solution:email-notifier \
  -r recipients=file://recipients.json \
  -r subject="Batch Notification"
```

## Learning Points

1. **Complex CLI Input**: JSON arrays with nested objects passed via `-r` flag
2. **Resolver Validation**: CEL expressions validate object structure before action runs
3. **Foreach Pattern**: Same action runs once per recipient with `{{ __item }}` binding
4. **Template Context**: Templates receive both resolver context (`_.senderName`) and foreach item (`__item.name`)
5. **API Integration**: Provider configuration references environment variables (`SENDGRID_API_KEY`)
6. **Conditional Actions**: `when` clause prevents unnecessary API calls with empty recipient list

## API Provider Configuration

The solution uses SendGrid API as an example:

```yaml
providers:
  - name: email-api
    type: api
    config:
      endpoint: https://api.sendgrid.com/v3/mail/send
      method: POST
      auth:
        type: bearer
        tokenEnv: SENDGRID_API_KEY
```

To use this example:
1. Create a SendGrid account and API key
2. Set environment variable: `export SENDGRID_API_KEY=your-api-key`
3. Run the solution

## Recipient Format

Recipients must be a valid JSON array of objects with `name` and `email` fields:

```json
[
  {"name": "Alice", "email": "alice@example.com"},
  {"name": "Bob", "email": "bob@example.com"}
]
```

The resolver validates that each object has both required fields before action execution.

## Testing

This solution includes inline tests for:

1. **Resolver defaults** - Verifies two default recipients and email settings
2. **Custom input** - Tests custom recipient list and subject override
3. **Dry-run** - Validates email payload generation without API calls

Run tests:

```bash
scafctl test solution:email-notifier --test resolve-defaults
scafctl test solution:email-notifier --test custom-recipients-and-subject
scafctl test solution:email-notifier --test dry-run-email-send
```

## See Also

- `notes/resolvers.md` - Resolver input sources and validation
- `notes/actions.md` - Action execution with foreach
- `notes/providers.md` - Provider types and API integration
- `notes/templates.md` - Template rendering and context
- `.github/copilot-instructions.md` - Architectural overview

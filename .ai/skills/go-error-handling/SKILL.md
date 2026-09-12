# Skill: Go Idiomatic Error Handling

Use this skill for error definitions, wrapping, and error matching.

## Guidelines
1. **Sentinel Errors**: Define sentinel errors as package-level variables:
   \x60\x60\x60go
   var ErrUserNotFound = errors.New("user not found")
   \x60\x60\x60
2. **Error Wrapping**: Wrap errors with context using \x60%w\x60:
   \x60\x60\x60go
   if err != nil {
       return fmt.Errorf("find user %d: %w", id, err)
   }
   \x60\x60\x60
3. **Error Matching**: Always use \x60errors.Is\x60 and \x60errors.As\x60:
   \x60\x60\x60go
   if errors.Is(err, ErrUserNotFound) { ... }
   \x60\x60\x60

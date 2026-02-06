package input

// ConfirmOptions configures the behavior of Confirm prompts.
type ConfirmOptions struct {
	Prompt        string
	Default       bool
	SkipCondition bool
}

// NewConfirmOptions creates default ConfirmOptions.
func NewConfirmOptions() *ConfirmOptions {
	return &ConfirmOptions{
		Prompt:        "Continue?",
		Default:       false,
		SkipCondition: false,
	}
}

// WithPrompt sets the prompt message.
func (o *ConfirmOptions) WithPrompt(prompt string) *ConfirmOptions {
	o.Prompt = prompt
	return o
}

// WithDefault sets the default value.
func (o *ConfirmOptions) WithDefault(def bool) *ConfirmOptions {
	o.Default = def
	return o
}

// WithSkipCondition sets whether to skip the prompt.
func (o *ConfirmOptions) WithSkipCondition(skip bool) *ConfirmOptions {
	o.SkipCondition = skip
	return o
}

// PasswordOptions configures the behavior of ReadPassword prompts.
type PasswordOptions struct {
	Prompt              string
	ConfirmPrompt       string
	RequireConfirmation bool
	MinLength           int
	AllowEmpty          bool
}

// NewPasswordOptions creates default PasswordOptions.
func NewPasswordOptions() *PasswordOptions {
	return &PasswordOptions{
		Prompt:              "Enter password: ",
		ConfirmPrompt:       "Confirm password: ",
		RequireConfirmation: false,
		MinLength:           0,
		AllowEmpty:          false,
	}
}

// WithPrompt sets the password prompt message.
func (o *PasswordOptions) WithPrompt(prompt string) *PasswordOptions {
	o.Prompt = prompt
	return o
}

// WithConfirmPrompt sets the confirmation prompt message.
func (o *PasswordOptions) WithConfirmPrompt(prompt string) *PasswordOptions {
	o.ConfirmPrompt = prompt
	return o
}

// WithConfirmation enables/disables password confirmation.
func (o *PasswordOptions) WithConfirmation(require bool) *PasswordOptions {
	o.RequireConfirmation = require
	return o
}

// WithMinLength sets the minimum password length.
func (o *PasswordOptions) WithMinLength(length int) *PasswordOptions {
	o.MinLength = length
	return o
}

// WithAllowEmpty sets whether empty passwords are allowed.
func (o *PasswordOptions) WithAllowEmpty(allow bool) *PasswordOptions {
	o.AllowEmpty = allow
	return o
}

// LineOptions configures the behavior of ReadLine prompts.
type LineOptions struct {
	Prompt     string
	Default    string
	AllowEmpty bool
	Validator  func(string) error
}

// NewLineOptions creates default LineOptions.
func NewLineOptions() *LineOptions {
	return &LineOptions{
		Prompt:     "Enter value: ",
		Default:    "",
		AllowEmpty: true,
		Validator:  nil,
	}
}

// WithPrompt sets the prompt message.
func (o *LineOptions) WithPrompt(prompt string) *LineOptions {
	o.Prompt = prompt
	return o
}

// WithDefault sets the default value.
func (o *LineOptions) WithDefault(def string) *LineOptions {
	o.Default = def
	return o
}

// WithAllowEmpty sets whether empty input is allowed.
func (o *LineOptions) WithAllowEmpty(allow bool) *LineOptions {
	o.AllowEmpty = allow
	return o
}

// WithValidator sets a validation function.
func (o *LineOptions) WithValidator(validator func(string) error) *LineOptions {
	o.Validator = validator
	return o
}

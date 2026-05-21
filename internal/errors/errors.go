// Package errors — typed errors.
//
// Bu paket runtime-da qarşılaşılan ümumi səhvləri kateqoriyalara bölür.
// CLI istifadəçiyə daha aydın mesajlar göstərə bilir.
package errors

import (
	"errors"
	"fmt"
)

// Sentinel errors — kategoriyalar.
//
// `errors.Is(err, ErrNotFound)` ilə yoxlamaq mümkündür.
var (
	ErrNotFound       = errors.New("tapılmadı")
	ErrAlreadyExists  = errors.New("artıq mövcuddur")
	ErrInvalidInput   = errors.New("yanlış giriş")
	ErrPermission     = errors.New("icazə yoxdur")
	ErrNotImplemented = errors.New("hələ implementasiya olunmayıb")
	ErrConflict       = errors.New("konflikt")
	ErrInternal       = errors.New("daxili xəta")
)

// E — yapışdırılmış error tipi.
//
// Kategori (sentinel), modul (paket adı), və əlavə context saxlayır.
type E struct {
	Kind    error  // sentinel error
	Module  string // "runtime", "image", "network", ...
	Op      string // "container start", "image pull", ...
	Wrapped error  // alt error
}

func (e *E) Error() string {
	parts := []string{}
	if e.Module != "" {
		parts = append(parts, e.Module)
	}
	if e.Op != "" {
		parts = append(parts, e.Op)
	}
	prefix := ""
	if len(parts) > 0 {
		prefix = fmt.Sprintf("[%s] ", joinStrings(parts, ":"))
	}

	if e.Wrapped != nil {
		return fmt.Sprintf("%s%s: %v", prefix, e.Kind, e.Wrapped)
	}
	return fmt.Sprintf("%s%s", prefix, e.Kind)
}

// Unwrap — `errors.Is` üçün.
func (e *E) Unwrap() error {
	return e.Wrapped
}

// Is — `errors.Is(err, ErrNotFound)` üçün.
func (e *E) Is(target error) bool {
	return errors.Is(e.Kind, target)
}

// New — yeni typed error.
func New(kind error, module, op string, wrapped error) *E {
	return &E{Kind: kind, Module: module, Op: op, Wrapped: wrapped}
}

// NotFound — qısa helper.
func NotFound(module, op string, wrapped error) *E {
	return New(ErrNotFound, module, op, wrapped)
}

// Invalid — yanlış giriş.
func Invalid(module, op string, wrapped error) *E {
	return New(ErrInvalidInput, module, op, wrapped)
}

// Internal — daxili xəta.
func Internal(module, op string, wrapped error) *E {
	return New(ErrInternal, module, op, wrapped)
}

// IsNotFound — error not-found-dırmı?
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}

func IsInvalid(err error) bool {
	return errors.Is(err, ErrInvalidInput)
}

// joinStrings — strings.Join-un re-implementation-u (import cycle qaçmaq üçün).
func joinStrings(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for _, p := range parts[1:] {
		result += sep + p
	}
	return result
}

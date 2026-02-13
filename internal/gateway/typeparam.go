package gateway

import ()

// Map 泛型函数，将任意集合转换为另一个集合
func Map[T, U any](slice []T, f func(T) U) []U {
	result := make([]U, len(slice))
	for i, v := range slice {
		result[i] = f(v)
	}
	return result
}

// Filter 泛型函数，过滤集合元素
func Filter[T any](slice []T, f func(T) bool) []T {
	var result []T
	for _, v := range slice {
		if f(v) {
			result = append(result, v)
		}
	}
	return result
}

// Find 泛型函数，查找符合条件的第一个元素
func Find[T any](slice []T, f func(T) bool) (T, bool) {
	for _, v := range slice {
		if f(v) {
			return v, true
		}
	}
	var zero T
	return zero, false
}

// Reduce 泛型函数，聚合操作
func Reduce[T, U any](slice []T, f func(U, T) U, initial U) U {
	result := initial
	for _, v := range slice {
		result = f(result, v)
	}
	return result
}

// KeyValuePair 表示键值对
type KeyValuePair[K comparable, V any] struct {
	Key   K
	Value V
}

// ToPairs 将 map 转换为键值对切片
func ToPairs[K comparable, V any](m map[K]V) []KeyValuePair[K, V] {
	result := make([]KeyValuePair[K, V], 0, len(m))
	for k, v := range m {
		result = append(result, KeyValuePair[K, V]{
			Key:   k,
			Value: v,
		})
	}
	return result
}

// ToMap 将键值对切片转换为 map
func ToMap[K comparable, V any](pairs []KeyValuePair[K, V]) map[K]V {
	result := make(map[K]V, len(pairs))
	for _, p := range pairs {
		result[p.Key] = p.Value
	}
	return result
}

// Option 是通用的可选值类型
type Option[T any] struct {
	value T
	valid bool
}

func Some[T any](v T) Option[T] {
	return Option[T]{
		value: v,
		valid: true,
	}
}

func None[T any]() Option[T] {
	return Option[T]{
		valid: false,
	}
}

func (o Option[T]) IsSome() bool {
	return o.valid
}

func (o Option[T]) IsNone() bool {
	return !o.valid
}

func (o Option[T]) Unwrap() T {
	if !o.valid {
		var zero T
		return zero
	}
	return o.value
}

func (o Option[T]) UnwrapOr(defaultValue T) T {
	if !o.valid {
		return defaultValue
	}
	return o.value
}

// Result 是通用的结果类型
type Result[T any, E any] struct {
	value T
	err   E
	ok    bool
}

func Ok[T any, E any](v T) Result[T, E] {
	return Result[T, E]{
		value: v,
		ok:    true,
	}
}

func Err[T any, E any](err E) Result[T, E] {
	return Result[T, E]{
		err: err,
		ok:  false,
	}
}

func (r Result[T, E]) IsOk() bool {
	return r.ok
}

func (r Result[T, E]) IsErr() bool {
	return !r.ok
}

func (r Result[T, E]) Unwrap() T {
	if !r.ok {
		var zero T
		return zero
	}
	return r.value
}

func (r Result[T, E]) UnwrapErr() E {
	return r.err
}

func (r Result[T, E]) UnwrapOr(defaultValue T) T {
	if !r.ok {
		return defaultValue
	}
	return r.value
}

// ConfigParser 泛型解析器接口
type ConfigParser[C any] interface {
	Parse(path string) (C, error)
	Validate(config C) error
}

// TOMLParser TOML 文件解析器
type TOMLParser[C any] struct{}

func (p *TOMLParser[C]) Parse(path string) (C, error) {
	var config C
	// 解析逻辑
	return config, nil
}

func (p *TOMLParser[C]) Validate(config C) error {
	// 验证逻辑
	return nil
}

// JSONParser JSON 文件解析器
type JSONParser[C any] struct{}

func (p *JSONParser[C]) Parse(path string) (C, error) {
	var config C
	// 解析逻辑
	return config, nil
}

func (p *JSONParser[C]) Validate(config C) error {
	// 验证逻辑
	return nil
}

// ParseConfig 使用指定的解析器解析配置
func ParseConfig[C any](parser ConfigParser[C], path string) (C, error) {
	cfg, err := parser.Parse(path)
	if err != nil {
		return cfg, err
	}
	return cfg, parser.Validate(cfg)
}

// ResultToOption 将结果转换为可选值
func ResultToOption[T any](r Result[T, error]) Option[T] {
	if r.IsOk() {
		return Some(r.Unwrap())
	}
	return None[T]()
}

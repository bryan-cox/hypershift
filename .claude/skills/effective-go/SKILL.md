---
name: Effective Go
description: "Apply Go best practices, idioms, and conventions from golang.org/doc/effective_go. Use when writing, reviewing, or refactoring Go code to ensure idiomatic, clean, and efficient implementations."
---

# Effective Go

Apply best practices and conventions from the official Effective Go guide to write clean, idiomatic Go code.

## Core Principles

### Do Not Communicate by Sharing Memory
**Share memory by communicating** - use channels to pass data between goroutines instead of shared variables with locks.

### Simplicity and Clarity
- Write clear, readable code over clever code
- Leverage Go's zero values
- Use the standard library as a reference for idiomatic patterns

## Formatting

**ALWAYS use `gofmt`** - this is non-negotiable in Go.
- Indentation: tabs, not spaces
- Line length: no strict limit, but wrap with extra tab indent
- Braces: same line as control structures (K&R style)

```go
// Correct
if x > 0 {
    return y
}

// Wrong
if x > 0
{
    return y
}
```

## Naming Conventions

### Package Names
- **Lowercase**, single word, concise
- NO underscores or mixedCase
- Named after what they provide, not what they contain
- Examples: `http`, `time`, `strings`, `bufio` (not `buffered_io`)

### Exported vs Unexported
- **Exported**: Start with uppercase (`MixedCaps`)
- **Unexported**: Start with lowercase (`mixedCaps`)
- NEVER use underscores in names

### Interface Names
- Single-method interfaces: method name + "er" suffix
  - `Reader`, `Writer`, `Formatter`, `CloseNotifier`
- Avoid stuttering: `io.Reader` not `io.ReadCloser` (unless it reads AND closes)

### Getters and Setters
- NO "Get" prefix for getters
- Use "Set" prefix for setters

```go
// Correct
owner := obj.Owner()
obj.SetOwner(user)

// Wrong
owner := obj.GetOwner()
```

## Commentary

### Package Documentation
- Every package should have a package comment
- For multi-file packages, put it in one file (typically `doc.go`)
- Start with "Package <name>" for godoc

```go
// Package regexp implements regular expression search.
package regexp
```

### Function Documentation
- Document ALL exported functions
- Start with the function name

```go
// Compile parses a regular expression and returns, if successful,
// a Regexp that can be used to match against text.
func Compile(str string) (*Regexp, error) {
```

## Control Structures

### If Statements
- No parentheses around conditions
- Braces are mandatory
- Use initialization statement when helpful

```go
// Initialization in if
if err := file.Chmod(0664); err != nil {
    log.Print(err)
    return err
}

// Multiple return values
if value, ok := myMap[key]; ok {
    // use value
}
```

### For Loops
Go has only `for`, which replaces `while`, `do-while`, and C-style `for`

```go
// Traditional
for i := 0; i < 10; i++ {
}

// While-style
for condition {
}

// Infinite
for {
}

// Range over slice/map
for key, value := range myMap {
}

// Ignore values with _
for key := range myMap {
}
for _, value := range slice {
}
```

### Switch
- Cases don't fall through (no break needed)
- Can switch on any type, not just integers
- Cases can be expressions

```go
// Type switch
switch t := value.(type) {
case *int:
    fmt.Printf("int: %d\n", *t)
case *string:
    fmt.Printf("string: %s\n", *t)
default:
    fmt.Printf("unknown type\n")
}

// Expression switch
switch {
case x < 0:
    return -1
case x == 0:
    return 0
default:
    return 1
}
```

## Functions

### Multiple Return Values
Use for returning both result and error, or result and success indicator

```go
func nextInt(b []byte, i int) (int, int) {
    // return value and next position
}

// Common pattern
func Read(p []byte) (n int, err error) {
}
```

### Named Result Parameters
- Useful for documentation
- Initialized to zero values
- Can be used as variables
- Bare `return` returns named parameters

```go
func ReadFull(r Reader, buf []byte) (n int, err error) {
    for len(buf) > 0 && err == nil {
        var nr int
        nr, err = r.Read(buf)
        n += nr
        buf = buf[nr:]
    }
    return // returns n and err
}
```

### Defer
- Executes function call when surrounding function returns
- LIFO order (last defer runs first)
- Perfect for cleanup, unlocking, closing

```go
func Contents(filename string) (string, error) {
    f, err := os.Open(filename)
    if err != nil {
        return "", err
    }
    defer f.Close()  // Will run when we return

    // ... read file ...
}

// Multiple defers - unlock happens after print
func trace(s string) string {
    fmt.Println("entering:", s)
    return s
}
func un(s string) {
    fmt.Println("leaving:", s)
}
func a() {
    defer un(trace("a"))
    fmt.Println("in a")
}
```

## Data Structures

### Allocation with new and make
- **new(T)**: allocates zeroed storage, returns `*T`
- **make(T, args)**: creates slices, maps, channels ONLY, returns initialized `T` (not `*T`)

```go
// new returns pointer to zero value
p := new(SyncedBuffer)  // type *SyncedBuffer, value is zero

// make returns initialized value
v := make([]int, 100)   // v is []int with len and cap 100

// Composite literal (preferred)
return &File{fd: fd, name: name}
```

### Slices
- Dynamically-sized, flexible view into arrays
- Hold references to underlying array
- Use `make` to create with capacity

```go
// Create with make
s := make([]int, 10)      // len=10, cap=10
s := make([]int, 0, 10)   // len=0, cap=10

// Append
s = append(s, 1, 2, 3)

// Two-dimensional slices
picture := make([][]uint8, YSize)
for i := range picture {
    picture[i] = make([]uint8, XSize)
}
```

### Maps
- Reference types
- Must use `make` before use (or composite literal)
- Comma-ok idiom for testing presence

```go
// Create
m := make(map[string]int)

// Composite literal
m := map[string]int{
    "route": 66,
    "foo":   42,
}

// Test for presence
value, ok := m["key"]
if !ok {
    // key not present
}

// Delete
delete(m, "key")
```

### Constants
- Use `iota` for enumerations
- Can be untyped for flexibility

```go
type ByteSize float64

const (
    _           = iota // ignore first value
    KB ByteSize = 1 << (10 * iota)
    MB
    GB
    TB
    PB
)
```

## Methods

### Pointer vs Value Receivers
**Pointer receiver when:**
- Method modifies the receiver
- Receiver is large struct (avoid copying)
- Consistency (if some methods have pointer receivers, all should)

**Value receiver when:**
- Receiver is small, simple value type
- Receiver should not be modified

```go
// Pointer receiver - can modify
func (p *Person) SetAge(age int) {
    p.Age = age
}

// Value receiver - read-only
func (p Person) String() string {
    return p.Name
}
```

### Method Expression
```go
// Method value
p := Point{1, 2}
f := p.ScaleBy  // method value bound to p
f(2)            // equivalent to p.ScaleBy(2)
```

## Interfaces

### Design Principles
- Keep interfaces small (1-3 methods ideal)
- Define interfaces in consumer packages, not implementer packages
- Accept interfaces, return concrete types

```go
// Small, focused interface
type Reader interface {
    Read(p []byte) (n int, err error)
}

// Composition of interfaces
type ReadWriter interface {
    Reader
    Writer
}
```

### Type Assertions and Type Switches
```go
// Type assertion
r, ok := value.(io.Reader)
if !ok {
    // handle error
}

// Type switch
switch v := value.(type) {
case string:
    fmt.Printf("string: %s\n", v)
case int:
    fmt.Printf("int: %d\n", v)
default:
    fmt.Printf("unknown\n")
}
```

### Empty Interface
`interface{}` holds any type, but use sparingly. Prefer concrete types or specific interfaces.

## Error Handling

### Error Type
```go
// Simple error
if err != nil {
    return err
}

// Wrap with context
if err != nil {
    return fmt.Errorf("failed to read config: %w", err)
}

// Custom error type
type PathError struct {
    Op   string
    Path string
    Err  error
}

func (e *PathError) Error() string {
    return e.Op + " " + e.Path + ": " + e.Err.Error()
}
```

### Panic and Recover
- Use panic for unrecoverable errors only
- Use recover in deferred functions
- Common in packages to convert internal panics to errors

```go
func server(workChan <-chan *Work) {
    for work := range workChan {
        go safelyDo(work)
    }
}

func safelyDo(work *Work) {
    defer func() {
        if err := recover(); err != nil {
            log.Println("work failed:", err)
        }
    }()
    do(work)
}
```

## Concurrency

### Goroutines
- Prefix function call with `go`
- Cheap, multiplexed onto OS threads

```go
go func() {
    // runs concurrently
}()

go list.Sort()  // Sort runs concurrently; don't wait for it
```

### Channels
- Typed conduits for communication
- Can be buffered or unbuffered

```go
// Unbuffered channel (synchronous)
ch := make(chan int)

// Buffered channel
ch := make(chan int, 100)

// Send
ch <- value

// Receive
value := <-ch

// Close
close(ch)

// Range over channel
for value := range ch {
    // receives until channel closed
}
```

### Select
- Multiplex on multiple channels

```go
select {
case msg := <-ch1:
    fmt.Println("received from ch1:", msg)
case msg := <-ch2:
    fmt.Println("received from ch2:", msg)
case <-time.After(time.Second):
    fmt.Println("timeout")
default:
    fmt.Println("no communication")
}
```

### Common Patterns

**Semaphore pattern:**
```go
var sem = make(chan int, MaxOutstanding)

func handle(r *Request) {
    sem <- 1    // Wait for active queue to drain
    process(r)  // May take a long time
    <-sem       // Done; enable next request to run
}

func Serve(queue chan *Request) {
    for req := range queue {
        go handle(req)
    }
}
```

**Worker pool:**
```go
func Serve(queue chan *Request) {
    for i := 0; i < MaxOutstanding; i++ {
        go func() {
            for req := range queue {
                process(req)
            }
        }()
    }
}
```

## Initialization

### Init Function
- Runs after package-level variables initialized
- Each file can have multiple init functions
- Runs in order of declaration

```go
func init() {
    // setup code
}
```

### Variable Initialization
```go
// Package-level variables initialized in declaration order
var (
    home   = os.Getenv("HOME")
    user   = os.Getenv("USER")
    gopath = os.Getenv("GOPATH")
)
```

## Common Anti-Patterns to Avoid

1. **Goroutine leaks** - always ensure goroutines can exit
2. **Copying mutexes** - use pointer receivers for types with sync.Mutex
3. **Closing channels prematurely** - only sender should close
4. **Ignoring errors** - always check and handle errors
5. **Stuttering names** - `buf.BufferSize` should be `buf.Size`
6. **Using panic for normal errors** - return errors instead

## Quick Checklist

When writing Go code, verify:
- [ ] Ran `gofmt` on all files
- [ ] All exported symbols documented
- [ ] Errors properly handled
- [ ] No goroutine leaks
- [ ] Used idiomatic names (no underscores, proper case)
- [ ] Channels closed by sender only
- [ ] Mutexes not copied (pointer receivers)
- [ ] Used `defer` for cleanup
- [ ] Interfaces are small and focused
- [ ] Returned errors, not panics (unless truly exceptional)

## References

- Official Guide: https://go.dev/doc/effective_go
- Code Review Comments: https://github.com/golang/go/wiki/CodeReviewComments
- Standard Library: Use as reference for idiomatic patterns

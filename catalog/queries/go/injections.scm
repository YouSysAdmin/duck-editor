((comment) @injection.content
 (#set! injection.language "comment"))

; Inject markdown into block comments right before a declaration. Line
; comments are left out: each line is its own node, a run of them gives
; no markdown worth parsing, and matching runs with (comment)* grows with
; the square of their length.
(source_file
  (comment) @injection.content
  .
  [
    (package_clause)
    (type_declaration)
    (function_declaration)
    (method_declaration)
    (var_declaration)
    (const_declaration)
    (var_spec)
    (const_spec)
  ]
  (#match? @injection.content "^/[*]")
  (#set! injection.language "markdown"))

((comment) @injection.content
 (#match? @injection.content "^//go:generate")
 (#set! injection.language "bash"))

(call_expression
  (selector_expression) @_function
  (#any-of? @_function "regexp.Match" "regexp.MatchReader" "regexp.MatchString" "regexp.Compile" "regexp.CompilePOSIX" "regexp.MustCompile" "regexp.MustCompilePOSIX")
  (argument_list
    .
    [
      (raw_string_literal (raw_string_literal_content) @injection.content)
      (interpreted_string_literal (interpreted_string_literal_content) @injection.content)
    ]
    (#set! injection.language "regex")))

; https://pkg.go.dev/fmt#Printf
; https://pkg.go.dev/fmt#Sprintf
; https://pkg.go.dev/fmt#Scanf
; https://pkg.go.dev/fmt#Errorf
((call_expression
  function: (selector_expression
    operand: (identifier) @_module
    field: (field_identifier) @_func)
  arguments: (argument_list
    . (interpreted_string_literal) @injection.content))
  (#eq? @_module "fmt")
  (#any-of? @_func "Printf" "Sprintf" "Scanf" "Errorf")
  (#set! injection.language "go-format-string"))

; https://pkg.go.dev/fmt#Fprintf
; https://pkg.go.dev/fmt#Fscanf
; https://pkg.go.dev/fmt#Sscanf
((call_expression
  function: (selector_expression
    operand: (identifier) @_module
    field: (field_identifier) @_func)
  arguments: (argument_list
    ; [(identifier) (interpreted_string_literal)]
    (_)
    ; (identifier)
    .
    (interpreted_string_literal) @injection.content))
  (#eq? @_module "fmt")
  (#any-of? @_func "Fprintf" "Fscanf" "Sscanf")
  (#set! injection.language "go-format-string"))

; https://pkg.go.dev/log#Printf
; https://pkg.go.dev/log#Fatalf
; https://pkg.go.dev/log#Panicf
; https://pkg.go.dev/log#Logger.Printf
; https://pkg.go.dev/log#Logger.Fatalf
; https://pkg.go.dev/log#Logger.Panicf
((call_expression
  function: (selector_expression
    operand: (identifier)
    field: (field_identifier) @_func)
  arguments: (argument_list
    . (interpreted_string_literal) @injection.content))
  (#any-of? @_func "Printf" "Fatalf" "Panicf")
  (#set! injection.language "go-format-string"))

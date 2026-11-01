((comment) @injection.content
 (#set! injection.language "comment"))

; The content of a file written whole, colored as what the file is: the first
; argument names the path, content = """...""" holds the text (Plumb recipes).

((call
  function: (identifier) @_fn
  arguments: (argument_list
    . (string (string_content) @_path)
    (keyword_argument
      name: (identifier) @_kw
      value: (string (string_content) @injection.content))))
  (#eq? @_fn "file")
  (#eq? @_kw "content")
  (#match? @_path "\\.(ya?ml)$")
  (#set! injection.language "yaml"))

((call
  function: (identifier) @_fn
  arguments: (argument_list
    . (string (string_content) @_path)
    (keyword_argument
      name: (identifier) @_kw
      value: (string (string_content) @injection.content))))
  (#eq? @_fn "file")
  (#eq? @_kw "content")
  (#match? @_path "\\.json$")
  (#set! injection.language "json"))

((call
  function: (identifier) @_fn
  arguments: (argument_list
    . (string (string_content) @_path)
    (keyword_argument
      name: (identifier) @_kw
      value: (string (string_content) @injection.content))))
  (#eq? @_fn "file")
  (#eq? @_kw "content")
  (#match? @_path "\\.toml$")
  (#set! injection.language "toml"))

((call
  function: (identifier) @_fn
  arguments: (argument_list
    . (string (string_content) @_path)
    (keyword_argument
      name: (identifier) @_kw
      value: (string (string_content) @injection.content))))
  (#eq? @_fn "file")
  (#eq? @_kw "content")
  (#match? @_path "(\\.sh|/profile\\.d/[^/]+|\\.bashrc|\\.profile)$")
  (#set! injection.language "bash"))

; exec and the scheduled commands.
((call
  function: (identifier) @_fn
  arguments: (argument_list
    (keyword_argument
      name: (identifier) @_kw
      value: (string (string_content) @injection.content))))
  (#any-of? @_fn "exec" "cron" "systemd_timer")
  (#eq? @_kw "command")
  (#set! injection.language "bash"))

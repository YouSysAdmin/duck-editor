; inherits: bash

; deb [options] uri suite components
((command_name (word) @keyword)
  (#any-of? @keyword "deb" "deb-src"))

(command
  argument: (word) @constant)

(command
  argument: (concatenation) @attribute)

((command
  argument: (word) @string.special.url)
  (#match? @string.special.url "^[a-z][a-z0-9+.-]*:"))

((command
  argument: (word) @_uri
  .
  argument: (word) @type)
  (#match? @_uri "^[a-z][a-z0-9+.-]*:"))

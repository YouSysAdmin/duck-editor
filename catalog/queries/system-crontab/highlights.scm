; inherits: bash

; Five schedule fields, the user, then the command.
((command
  name: (command_name (_) @constant.numeric)
  .
  argument: (_) @constant.numeric
  .
  argument: (_) @constant.numeric
  .
  argument: (_) @constant.numeric
  .
  argument: (_) @constant.numeric
  .
  argument: (_) @variable.builtin
  .
  argument: (_) @function)
  (#not-match? @constant.numeric "^@"))

; An @ nickname such as @daily, the user, then the command.
((command
  name: (command_name (_) @keyword.directive)
  .
  argument: (_) @variable.builtin
  .
  argument: (_) @function)
  (#match? @keyword.directive "^@"))

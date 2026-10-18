; inherits: bash

; Five schedule fields, then the command.
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
  argument: (_) @function)
  (#not-match? @constant.numeric "^@"))

; An @ nickname such as @daily, then the command.
((command
  name: (command_name (_) @keyword.directive)
  .
  argument: (_) @function)
  (#match? @keyword.directive "^@"))

[
    (single_line_comment)
    (multi_line_comment)

    (node_comment)
    (node_field_comment)

    (node . (node_comment))
    (node_field . (node_field_comment))
] @comment

(node
    (identifier) @variable)

(prop (identifier) @attribute)

(type (_) @type) @punctuation.bracket

(keyword) @keyword

(string) @string
(number) @constant.numeric
(boolean) @constant.builtin.boolean

"." @punctuation.delimiter

"=" @operator

"{" @punctuation.bracket
"}" @punctuation.bracket

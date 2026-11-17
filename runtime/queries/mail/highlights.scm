; Field names, grouped by what they say about the message.

(label_address) @keyword
(label_subject) @keyword
(label_date) @keyword
(label_msgid) @keyword
(label_id) @keyword

(label_received) @function
(label_ip) @function

(label_auth) @function.builtin
(label_spf) @function.builtin
(label_ms) @function.builtin
(label_spam) @function.builtin

(label_signature) @constructor

(label_content) @type
(label_flag) @type

(label_list) @label

(label_other) @variable

; Keys inside a structured value.

(trace_keyword) @attribute
(auth_name) @attribute
(property_name) @attribute
(tag_name) @attribute
(blob_name) @attribute
(param_name) @attribute
(ms_name) @attribute
(spam_name) @attribute

; Values worth finding at a glance.

(address) @string
(quoted_string) @string
(msgid) @string.special
(id_value) @string.special
(encoded_word) @string.special
(mime_type) @string.special
(uri) @string.special.url
(ip_address) @constant
(spam_value) @constant
(flag_value) @constant
(date_time) @variable.parameter
(authserv_id) @namespace
(boundary) @punctuation.special

; Signature blobs, provider state and parenthesised remarks carry nothing
; to read.

(comment) @comment
(blob_value) @comment
(label_opaque) @comment
(opaque) @comment

; Results of an authentication method.

(auth_result) @constant

((auth_result) @diff.plus
  (#any-of? @diff.plus "pass" "Pass" "PASS" "bestguesspass"))

((auth_result) @diff.minus
  (#any-of? @diff.minus
    "fail" "Fail" "FAIL" "softfail" "permerror" "temperror" "reject" "discard" "hardfail"))

((auth_result) @warning
  (#any-of? @warning "none" "None" "NONE" "neutral" "policy" "unknown" "error"))

; Verdicts of a spam or virus filter, with or without the comma a
; checker writes after the first word.

(spam_word) @constant

((spam_word) @diff.plus
  (#match? @diff.plus "^([Nn][Oo]|[Cc][Ll][Ee][Aa][Nn]|[Hh][Aa][Mm]|[Oo][Kk])[,;]?$"))

((spam_word) @diff.minus
  (#match? @diff.minus
    "^([Yy][Ee][Ss]|[Ss][Pp][Aa][Mm]|[Ii][Nn][Ff][Ee][Cc][Tt][Ee][Dd]|FOUND|[Rr][Ee][Jj][Ee][Cc][Tt])[,;]?$"))

; Flags that change how a message is treated.

((flag_value) @warning
  (#any-of? @warning
    "bulk" "junk" "auto-generated" "auto-replied" "high" "High" "HIGH" "urgent" "Urgent"
    "true" "True" "yes" "Yes" "on" "1"))

; Punctuation. Parentheses stay part of the remark they open.

[":" ";" ","] @punctuation.delimiter

["<" ">"] @punctuation.bracket

"=" @operator

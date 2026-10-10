; Ours, appended after the grammars' tags.scm (see README.md).

; Constructors: javascript/tags.scm leaves them out with
; (#not-eq? @name "constructor").
(method_definition
  name: (property_identifier) @name
  (#eq? @name "constructor")) @definition.method

; Class fields holding a function: handler = (e) => { … }
(public_field_definition
  name: (property_identifier) @name
  value: [(arrow_function) (function_expression)]) @definition.method

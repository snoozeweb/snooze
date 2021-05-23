const TokenType = {
  PAR_OPEN:     '(',
  PAR_CLOSE:    ')',
  EXISTS:       'EXISTS',
  NOT:          'NOT',
  MATCHES:      'MATCHES',
  CONTAINS:     'CONTAINS',
  EQUAL:        '=',
  NEQUAL:       '!=',
  EXCLAMATION:  '!',
  GREATER_THAN: '>',
  LOWER_THAN:   '<',
  AND:          'AND',
  OR:           'OR',
  LITERAL:      'LITERAL'
};

module.exports = TokenType;

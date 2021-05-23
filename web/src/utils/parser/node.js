const TokenType = require('./token-type');

const make = gen => {
  const data = gen.next().value;

  if (!data) {
    // TO DO: Throw Syntax Error
    return null;
  }

  switch (data.type) {
    case TokenType.LITERAL:
      return '"' + data.value + '"';
    case TokenType.EXISTS:
    case TokenType.NOT:
      return '["' + data.type + '", ' + make(gen) + ']';
    case TokenType.MATCHES:
    case TokenType.CONTAINS:
    case TokenType.EQUAL:
    case TokenType.NEQUAL:
    case TokenType.GREATER_THAN:
    case TokenType.LOWER_THAN:
    case TokenType.OR:
    case TokenType.AND: {
      const left = make(gen);
      const right = make(gen);
      return '["' + data.type + '", ' + left + ', ' + right + ']';
    }
  }
  return null;
};


module.exports = {
  make
};

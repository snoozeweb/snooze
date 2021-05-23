const TokenType = require('./token-type');

const Tokenizer = (exp) => {
  let literal = '';
  const tokens = [];

  const addToken = (type, value) => tokens.push({ type, value });

  const addTokenFromLiteral = () => {
    if (literal !== '') {
      const code = literal.trim().toUpperCase();
      switch (code) {
        case TokenType.EXISTS:
        case TokenType.NOT:
        case TokenType.MATCHES:
        case TokenType.CONTAINS:
        case TokenType.EQUAL:
        case TokenType.NEQUAL:
        case TokenType.GREATER_THAN:
        case TokenType.LOWER_THAN:
        case TokenType.AND:
        case TokenType.OR:
          addToken(code, literal);
          break;
        default:
          addToken(TokenType.LITERAL, literal);
      }
      literal = '';
    }
  };

  for (const char of exp) {
    switch (char) {
      case TokenType.PAR_OPEN:
      case TokenType.PAR_CLOSE:
        addTokenFromLiteral();
        addToken(char, char);
        break;
      case TokenType.EXCLAMATION:
        addTokenFromLiteral();
        literal = char;
        break;
      case TokenType.EQUAL: {
        if(literal != TokenType.EXCLAMATION) {
          addTokenFromLiteral();
        }
        literal += char;
        addTokenFromLiteral();
        break;
      }
      case TokenType.GREATER_THAN:
      case TokenType.LOWER_THAN:
        addTokenFromLiteral();
        literal += char;
        addTokenFromLiteral();
        break;
      default:
        if (/\s/g.test(char)) {
          addTokenFromLiteral();
        } else {
          literal += char;
        }
    }
  }

  addTokenFromLiteral();
  return tokens;
};

module.exports = Tokenizer;

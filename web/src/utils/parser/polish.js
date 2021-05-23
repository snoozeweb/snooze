const TokenType = require('./token-type');

const PolishNotation = tokens => {
  const queue = [];
  const stack = [];
  tokens.reverse().forEach(token => {
    switch (token.type) {
      case TokenType.LITERAL:
        queue.unshift(token);
        break;
      case TokenType.EXISTS:
      case TokenType.NOT:
      case TokenType.MATCHES:
      case TokenType.CONTAINS:
      case TokenType.EQUAL:
      case TokenType.NEQUAL:
      case TokenType.GREATER_THAN:
      case TokenType.LOWER_THAN:
      case TokenType.AND:
      case TokenType.OR: {
          while (
            stack.length &&
            IsOperator(stack[stack.length - 1]) &&
            GetPrecedence(stack[stack.length - 1]) > GetPrecedence(token)
          ) {
            queue.unshift(stack.pop());
          }
          stack.push(token);
          break;
        }
      case TokenType.PAR_CLOSE:
        stack.push(token);
        break;
      case TokenType.PAR_OPEN: {
        while (
          stack.length &&
          stack[stack.length - 1].type !== TokenType.PAR_CLOSE
        ) {
          queue.unshift(stack.pop());
        }

        stack.pop();
        break;
        }
      default:
        break;
    }
  });

  const result = (stack.length && [...stack, ...queue]) || queue;
  return result;
};

const PolishGenerator = function*(polish) {
  for (let index = 0; index < polish.length - 1; index++) {
    yield polish[index];
  }

  return polish[polish.length - 1];
};

const GetPrecedence = token => {
  switch (token.type) {
    case TokenType.PAR_OPEN:
    case TokenType.PAR_CLOSE:
      return 14;
    case TokenType.EXISTS:
      return 13;
    case TokenType.NOT:
      return 11;
    case TokenType.MATCHES:
    case TokenType.CONTAINS:
      return 10;
    case TokenType.EQUAL:
    case TokenType.NEQUAL:
    case TokenType.GREATER_THAN:
    case TokenType.LOWER_THAN:
      return 8;
    case TokenType.AND:
      return 5;
    case TokenType.OR:
      return 4;
    default:
      return 0;
  }
}

const IsOperator = token => {
  switch (token.type) {
    case TokenType.PAR_OPEN:
    case TokenType.PAR_CLOSE:
      return false;
    default:
      return true;
  }
}

module.exports = {
  PolishNotation,
  PolishGenerator
};

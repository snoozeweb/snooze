const Tokenizer = require('./tokenizer');
const Polish = require('./polish');
const Node = require('./node');

const parse = (exp) => {
  const tokens = Tokenizer(exp);
  const polish = Polish.PolishNotation(tokens);
  const gen = Polish.PolishGenerator(polish);
  return Node.make(gen);
};

module.exports = { parse };

// Functions related to condition transformation

import { uuid } from 'vue-uuid'

export const OPERATION_TYPE = {
  '=': 'binary',
  '!=': 'binary',
  '>': 'binary',
  '>=': 'binary',
  '<': 'binary',
  '<=': 'binary',
  'MATCHES': 'binary',
  'CONTAINS': 'binary',
  'EXISTS': 'unary',
  'SEARCH': 'unary',
  'AND': 'logic',
  'OR': 'logic',
  'NOT': 'not',
  '': 'alwaysTrue',
  null: 'alwaysTrue',
  undefined: 'alwaysTrue',
}

export const OPERATION_SYMBOL = {
  'MATCHES': '~',
  'SEARCH': 'unary',
  'AND': '&',
  'OR': '|',
  'NOT': '!',
}

export class ConditionObject {
  constructor(op, args) {
    //console.log(`ConditionObject(${op}, ${args})`)
    this.id = uuid.v4()
    this.args = args
    this.operation = op
  }
  static fromArray(array) {
    //console.log(`ConditionObject.fromArray(${array})`)
    if (array === undefined) {
      return new ConditionObject('', [])
    }
    var operation = array[0]
    if (OPERATION_TYPE[operation] == 'logic' || operation == 'NOT') {
      var args = array.slice(1).map(arg => ConditionObject.fromArray(arg))
    } else {
      var args =  array.slice(1)
    }
    //console.log(`Operation: ${operation}`)
    //console.log(`Args: ${args}`)
    var c = new ConditionObject(operation, args)
    //console.log(`ConditionObject.fromArray: ${c}`)
    return c
  }
  get type() {
    return OPERATION_TYPE[this.operation]
  }
  get operationSymbol() {
    return OPERATION_SYMBOL.hasOwnProperty(this.operation) ? OPERATION_SYMBOL[this.operation] : this.operation
  }
  toJSON() {
    var json = {operation: this.operation, id: this.id}
    if (this.type == 'logic') {
      json['args'] = this.args.map(arg => arg.toJSON())
    } else {
      json['args'] = this.args
    }
    return json
  }
  toArray() {
    if (this.type == 'logic' || this.type == 'not') {
      return [this.operation].concat(this.args.map(arg => arg.toArray()))
    } else {
      return [this.operation].concat(this.args)
    }
  }
  toSearch() {
    switch(this.type) {
      case 'logic':
        return "(" + this.args.map(arg => arg.toSearch()).join(` ${this.operationSymbol} `) + ")"
      case 'binary':
        return `${this.args[0]} ${this.operationSymbol} ${JSON.stringify(this.args[1])}`
      case 'unary':
        return `(${this.operation} ${JSON.stringify(this.args[0])})`
      case 'not':
        return `(!${this.args[0]})`
      case 'alwaysTrue':
        return '()'
      default:
        return `Invalid condition`
    }
  }
  toHTML() {
    switch(this.type) {
      case 'logic':
        return "(" + this.args.map(arg => arg.toHTML()).join(` <b>${this.operation}</b> `) + ")"
      case 'binary':
        return `${this.args[0]} <b>${this.operationSymbol}</b> ${JSON.stringify(this.args[1])}`
      case 'unary':
        return `(<b>${this.operation}</b> ${JSON.stringify(this.args[0])})`
      case 'not':
        return `<b>NOT</b> ${this.args[0].toHTML()}`
      case 'alwaysTrue':
        return '<b>Always true</b>'
      default:
        return `<b>Invalid Condition</b>`
    }
  }
  combine(operator, other) {
    //console.log(`ConditionObject.combine(${operator}, ${other})`)
    return new ConditionObject(operator, [this, other])
  }
  toString() {
    return JSON.stringify(this.toJSON())
  }
}

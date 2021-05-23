import { colors } from '@/objects/Field.yaml'

export function gen_color(field) {
  var role = this.items.filter(opt => opt['name'] == field)[0]
  if('color' in role) {
    var color = hexToRgb(role['color'])
    var fontcolor = (color.r*0.299 + color.g*0.587 + color.b*0.114) > 186 ? '#4f5d73' : '#ffffff'
    return 'background-color: ' + role['color'] + ' !important;color: ' + fontcolor + ' !important'
  } else {
    return ''
  }
}

export function hexToRgb(hex) {
  var result = /^#?([a-f\d]{2})([a-f\d]{2})([a-f\d]{2})$/i.exec(hex);
  return result ? {
    r: parseInt(result[1], 16),
    g: parseInt(result[2], 16),
    b: parseInt(result[3], 16)
  } : null;
}

export function get_color(field) {
  if(field in colors) {
    return colors[field]
  } else {
    return 'secondary'
  }
}

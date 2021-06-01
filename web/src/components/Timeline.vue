<template>
<span>
  {{ data }}
</span>
</template>

<script>

export default {
  name: 'Timeline',
  props: {
    data: {type: [Array, String]},
  },
}
</script>

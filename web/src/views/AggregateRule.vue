<template>
  <div class="animated fadeIn">
    <List
      endpoint_prop="aggregaterule"
      edit_mode
      delete_mode
      add_mode
    />
  </div>
</template>

<script>
import List from '@/components/List.vue'

export default {
  components: {
    List,
  },
}
</script>

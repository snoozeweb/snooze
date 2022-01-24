<template>
  <div class="animated fadeIn">
    <List
      endpoint_prop="kv"
      edit_mode
      delete_mode
      add_mode
    >
    </List>
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

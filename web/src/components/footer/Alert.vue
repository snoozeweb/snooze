<template>
  <div>
    <List
      ref="table"
      endpoint_prop="alert"
      :page_options_prop="page_options"
      no_search
      no_history
      no_selection
      short_contextmenu
      default_tab="Preview"
      @update="on_update"
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
  emits: ['feedback'],
  props: {
    data: {type: Object},
  },
  mounted () {
    this.handler['condition_refresh'] = condition => {
      this.$refs.table.default_search = JSON.stringify(condition.toArray())
      if (this.$refs.table.loaded) {
        this.$refs.table.reload()
      }
    }
    this.emitter.on('condition_refresh', this.handler['condition_refresh'])
  },
  unmounted () {
    this.emitter.off('condition_refresh', this.handler['condition_refresh'])
  },
  data () {
    return {
      show_modal: false,
      handler: {},
    }
  },
  computed: {
    page_options: function() {
      return ['5', '10', '20']
    },
  },
  methods: {
    on_update(val) {
      this.$emit('feedback', this.$refs.table.nb_rows)
    },
  },
}
</script>

<template>
  <div>
  <CHeaderNavItem v-for="widget in widgets" v-bind:key="widget.name">
    <component
      v-if="widget.vue_component"
      v-bind:is="widget.vue_component"
      :id="'component_'+widget.vue_component"
      :options="widget"
    />
  </CHeaderNavItem>
  </div>
</template>

<script>
import { API } from '@/api'

import PatliteWidget from '@/components/PatliteWidget'

// Create a card fed by an API endpoint.
export default {
  name: 'WidgetList',
  props: {
  },
  components: {
    PatliteWidget,
  },
  mounted () {
    this.listWidgets()
  },
  data () {
    return {
      widgets: [],
    }
  },
  computed: {
  },
  methods: {
    /**
     * Get the list of widgets from the API.
     * Update the `widgets` variable if the API return a result.
     */
    listWidgets() {
      API
        .get('/widget')
        .then(response => {
          if (response.data !== undefined && response.data.data !== undefined) {
            var widgets = response.data.data
            this.widgets = widgets.filter(widget => widget.enabled && widget.vue_component !== undefined)
          }
        })
        .catch(error => console.log(error))
    },
  },
  watch: {
  },
}
</script>

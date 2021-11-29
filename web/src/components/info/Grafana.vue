<template>
  <div>
    <CCard header="Grafana" header-class='text-center font-weight-bold'>
      <div class='text-center'>
        <a :href="data.rule_url">
          <img :src="data.image_url" class="rounded border border-danger" style="max-width:100%; max-height: 100%;"/>
        </a>
      </div>
    </CCard>
  </div>
</template>

<script>

import { more } from '@/utils/api'

export default {
  name: 'Mail',
  props: {
    data: {type: Object},
  },
  data () {
    return {
    }
  },
}
</script>

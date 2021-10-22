<template>
  <CChartDoughnut
    :datasets="data.sets"
    :labels="data.labels"
    :options="default_options"
  />
</template>

<script>
import { CChartDoughnut } from '@coreui/vue-chartjs'
import { gen_palette, hexToRgba } from '@/utils/colors'

export default {
  name: 'ChartDoughnut',
  components: { CChartDoughnut },
  props: {
    datasets: {
      type: Object,
      default: () => {},
    },
  },
  computed: {
    default_options () {
      return {
        maintainAspectRatio: false,
      }
    },
    data () {
      if (this.datasets) {
        var series_array = Object.keys(this.datasets).map(key => [key, this.datasets[key]])
        series_array = series_array.sort((a, b) => (a[1] < b[1]) ? 1: -1)
        var palette = gen_palette(series_array.length)
        return {
          sets: [{
            borderColor: palette,
            backgroundColor: palette.map(x => hexToRgba(x, 75)),
            data: series_array.map(x => x[1]),
          }],
          labels: series_array.map(x => x[0]),
        }
      } else {
        return {sets:[], labels: []}
      }
    },
  }
}
</script>

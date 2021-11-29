<template>
  <div :class="{ 'chart-wrapper': wrapper }">
		<canvas
			:id="id"
			:height="height"
			:width="width"
			@click="handleOnClick"
			role="img"
			ref="canvas"
		>
		</canvas>
  </div>
</template>

<script>
import { h, onMounted, onUnmounted, onUpdated, ref, computed } from 'vue'
import Chart, { ChartData, ChartOptions, ChartType, Plugin } from 'chart.js/auto'
import * as chartjs from 'chart.js'
import { customTooltips } from '@coreui/chartjs'

export default {
    name: 'SChart',
    emits: ['getDatasetAtEvent', 'getElementAtEvent', 'getElementsAtEvent'],
    props: {
      customTooltips: {
        type: Boolean,
        default: false,
        required: false,
      },
      data: {
        type: [Object, Function],
        required: true,
      },
      height: {
        type: Number,
        default: 150,
        required: false,
      },
      id: {
        type: String,
        default: undefined,
        required: false,
      },
      options: {
        type: Object,
        default: undefined,
        required: false,
      },
      plugins: {
        type: Array,
        default: undefined,
      },
      redraw: Boolean,
      type: {
        type: String,
        default: 'bar',
        required: false,
      },
      width: {
        type: Number,
        default: 300,
        required: false,
      },
      wrapper: {
        type: Boolean,
        default: true,
        required: false,
      },
    },
    setup(props, { emit, slots, expose }) {
      const canvasRef = ref()
      let chart = null

      const computedData = () => {
        return typeof props.data === 'function'
          ? canvasRef.value
            ? props.data(canvasRef.value)
            : { datasets: [] }
          : Object.assign({}, props.data)
      }

      const renderChart = () => {
        if (!canvasRef.value) return

        if (props.customTooltips) {
          chartjs.defaults.plugins.tooltip.enabled = false
          chartjs.defaults.plugins.tooltip.mode = 'index'
          chartjs.defaults.plugins.tooltip.position = 'nearest'
          chartjs.defaults.plugins.tooltip.external = customTooltips
        }
        chartjs.defaults.plugins.tooltip.bodySpacing = 5
        chartjs.defaults.plugins.tooltip.boxPadding = 5

        chart = new Chart(canvasRef.value, {
          type: props.type,
          data: computedData(),
          options: props.options,
          plugins: props.plugins,
        })
      }

      const handleOnClick = (e) => {
        if (!chart) return

        emit(
          'getDatasetAtEvent',
          chart.getElementsAtEventForMode(e, 'dataset', { intersect: true }, false),
          e,
        )
        emit(
          'getElementAtEvent',
          chart.getElementsAtEventForMode(e, 'nearest', { intersect: true }, false),
          e,
        )
        emit(
          'getElementsAtEvent',
          chart.getElementsAtEventForMode(e, 'index', { intersect: true }, false),
          e,
        )
      }

      const updateChart = () => {
        if (!chart) return

        if (props.options) {
          chart.options = { ...props.options }
        }

        if (!chart.config.data) {
          chart.config.data = computedData()
          chart.update()
          return
        }

        const { datasets: newDataSets = [], ...newChartData } = computedData()
        const { datasets: currentDataSets = [] } = chart.config.data

        // copy values
        Object.assign(chart.config.data, newChartData)
        chart.config.data.datasets = newDataSets.map((newDataSet) => {
          // given the new set, find it's current match
          const currentDataSet = currentDataSets.find(
            (d) => d.label === newDataSet.label && d.type === newDataSet.type,
          )

          // There is no original to update, so simply add new one
          if (!currentDataSet || !newDataSet.data) return newDataSet

          if (!currentDataSet.data) {
            currentDataSet.data = []
          } else {
            currentDataSet.data.length = newDataSet.data.length
          }

          // copy in values
          Object.assign(currentDataSet.data, newDataSet.data)

          // apply dataset changes, but keep copied data
          return {
            ...currentDataSet,
            ...newDataSet,
            data: currentDataSet.data,
          }
        })
        chart && chart.update()
      }

      const destroyChart = () => {
        if (chart) chart.destroy()
      }

      onMounted(() => {
        renderChart()
      })

      onUnmounted(() => {
        destroyChart()
      })

      onUpdated(() => {
        if (props.redraw) {
          destroyChart()
          setTimeout(() => {
            renderChart()
          }, 0)
        } else {
          updateChart()
        }
      })

      const canvas = (ref) =>
        h(
          'canvas',
          {
            id: props.id,
            height: props.height,
            width: props.width,
            onClick: (e) => handleOnClick(e),
            role: 'img',
            ref: ref,
          },
          {
            fallbackContent: () => slots.fallback && slots.fallback(),
          },
        )

      expose({
        updateChart
      })

      return () =>
        props.wrapper ? h('div', { class: 'chart-wrapper' }, canvas(canvasRef)) : canvas(canvasRef)
    },
}
</script>

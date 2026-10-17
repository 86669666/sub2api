<template>
  <main class="bg-app text-gray-900 dark:text-gray-100" style="padding:16px;min-height:100vh">
    <h1>CI component fixture — production components and CSS</h1>
    <button id="theme-toggle" class="btn btn-secondary" @click="toggleTheme">Toggle theme</button>
    <section id="chart-fixture" style="margin-top:16px">
      <TokenUsageTrend :trend-data="trend" :loading="state === 'loading'" />
    </section>
    <section style="margin-top:24px">
      <label>Filter fixture rows <input id="row-filter" v-model="filter" class="input" @input="page = 1" /></label>
      <div id="table-frame" style="height:400px;display:flex;flex-direction:column;margin-top:8px">
        <DataTable ref="table" :data="pageRows" :columns="columns" row-key="id" selectable
          :selected-keys="selected" :loading="state === 'loading'" :actions-count="4"
          @update:selected-keys="selected = $event">
          <template #cell-name="{ row }">
            <div style="white-space:normal;width:180px" :data-fixture-id="row.id">{{ row.name }}<br v-if="row.id % 3 === 0" /><span v-if="row.id % 3 === 0">Second line and variable row height</span></div>
          </template>
          <template #cell-actions="{ row }">
            <div>
              <button class="btn btn-secondary" :data-action="row.id" :aria-expanded="openAction === row.id" @click.stop="openAction = openAction === row.id ? null : row.id">Actions {{ row.id }}</button>
              <div v-if="openAction === row.id" role="menu" style="white-space:normal">
                <button role="menuitem" @click="lastAction = row.id; openAction = null">Inspect row {{ row.id }}</button>
              </div>
            </div>
          </template>
        </DataTable>
      </div>
      <Pagination v-if="paginated" v-model:page="page" :total="filtered.length" :page-size="101" :show-page-size-selector="false" />
      <output id="selection">{{ selected.join(',') }}</output><output id="last-action">{{ lastAction }}</output>
    </section>
  </main>
</template>
<script setup>
import { computed, nextTick, onMounted, ref } from 'vue'
import { Chart } from 'chart.js'
import TokenUsageTrend from '../../../frontend/src/components/charts/TokenUsageTrend.vue'
import DataTable from '../../../frontend/src/components/common/DataTable.vue'
import Pagination from '../../../frontend/src/components/common/Pagination.vue'
import { getDeploymentContract } from '../../../frontend/src/api/url'
import { trendFixture } from './visual-data.mjs'
const trend=ref(trendFixture()), count=ref(5), state=ref('normal'), filter=ref(''), page=ref(1), paginated=ref(false)
const table=ref(), selected=ref([]), openAction=ref(null), lastAction=ref(null)
const columns=[{key:'id',label:'ID',sortable:true},{key:'name',label:'Long multi-line name',sortable:true},{key:'amount',label:'Amount',sortable:true},{key:'actions',label:'Actions'}]
const allRows=computed(()=>Array.from({length:count.value},(_,i)=>({id:i+1,name:`Row ${i+1} — long fixture description`,amount:`$${(1234567.89+i).toFixed(2)}`})))
const filtered=computed(()=>allRows.value.filter(row=>row.name.toLowerCase().includes(filter.value.toLowerCase())))
const pageRows=computed(()=>state.value==='empty'?[]:paginated.value?filtered.value.slice((page.value-1)*101,page.value*101):filtered.value)
function toggleTheme(){document.documentElement.classList.toggle('dark')}
function chart(){return Chart.getChart(document.querySelector('#chart-fixture canvas'))}
// Observability belongs only to this CI entry. No production components get test props.
onMounted(()=>Object.assign(window,{visual:{
 contract:getDeploymentContract(),
 async configure(options){
  if(options.kind)trend.value=trendFixture(options.kind)
  if(options.count!==undefined){count.value=options.count;filter.value='';page.value=1;selected.value=[]}
  if(options.paginated!==undefined){paginated.value=options.paginated;page.value=1}
  if(options.state){state.value=options.state;trend.value=options.state==='empty'?[]:trendFixture(options.kind)}
  await nextTick()
 },
 chart,
 table(){const t=table.value;return {virtual:t.shouldVirtualize,ids:t.sortedData.map(row=>row.id),selected:[...selected.value],page:page.value,openAction:openAction.value,lastAction:lastAction.value}},
}}))
</script>

# List

Create a table representing an API endpoint.

## Props

<!-- @vuese:List:props:start -->
|Name|Description|Type|Required|Default|
|---|---|---|---|---|
|tabs|The tabs name and their associated search|`Object`|`true`|-|
|endpoint|The API path to query|`String`|`true`|-|
|fields|An array containing the fields to pass to the `b-table`|`Array`|`true`|-|
|form|An object describing the input form for editing/adding|`Object`|`true`|-|
|add_mode|Allow the `Add` button|`Boolean`|`false`|true|
|edit_mode|Allow the `Edit` button in actions|`Boolean`|`false`|true|
|delete_mode|Allow the `Delete` button in actions|`Boolean`|`false`|true|
|order_by|The default key to order by|`String`|`false`|undefined|

<!-- @vuese:List:props:end -->


## Events

<!-- @vuese:List:events:start -->
|Event Name|Description|Parameters|
|---|---|---|
|row-selected|Emit the selected rows from the `b-table`|-|

<!-- @vuese:List:events:end -->


## Slots

<!-- @vuese:List:slots:start -->
|Name|Description|Default Slot Content|
|---|---|---|
|head_buttons|Slots for placing additional buttons in the header of the table|-|
|actions|Action buttons|-|
|b-table-templates|Slots for the b-table|-|

<!-- @vuese:List:slots:end -->


